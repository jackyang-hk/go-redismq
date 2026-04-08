# go-redismq

[![Go Reference](https://pkg.go.dev/badge/github.com/jackyang-hk/go-redismq.svg)](https://pkg.go.dev/github.com/jackyang-hk/go-redismq)

基于 **Redis Streams** 与 **有序集合延迟队列** 实现的 Go 消息队列库：用 Redis 即可完成发布订阅、消费组竞争消费、延迟与重试、可选事务型发送以及类 RPC 调用，无需单独部署 MQ 进程。

**[English](./README.md)**

---

## 目录

- [功能概览](#功能概览)
- [环境与依赖](#环境与依赖)
- [安装](#安装)
- [快速开始](#快速开始)
- [配置说明](#配置说明)
- [核心概念](#核心概念)
- [生产消息](#生产消息)
- [消费消息](#消费消息)
- [重试与延迟调度](#重试与延迟调度)
- [事务型发送](#事务型发送)
- [Invoke（类 RPC）](#invoke类-rpc)
- [可观测性（可选）](#可观测性可选)
- [测试](#测试)
- [变更日志](#变更日志)

---

## 功能概览

| 能力 | 说明 |
|------|------|
| **传输层** | 按 topic 使用 Redis Stream 持久化；消费组（consumer group）支持多实例竞争消费。 |
| **路由** | 使用 **`topic` + `tag`** 定位业务；每个监听器注册在一个 `(topic, tag)` 上。 |
| **延迟** | 首次定时投递可通过 `StartDeliverTime`；`ReconsumeLater` 走内部 ZSET 延迟队列（`MQ_DELAY_QUEUE_SET`）。 |
| **重试** | 支持最大重试次数；默认退避约为「`60 × ReconsumeTimes` 秒」线性基数 + **有上限随机抖动**；消息上可选 `NextDeliverAt`、`NextRetryDelaySeconds` 精细控制。 |
| **兜底** | 超过重试上限进入 **死亡队列 Stream**；事务型发送提供 prepare / commit / rollback 路径。 |
| **Invoke** | 在同一套 Redis 设施上提供请求/应答式调用（内部 topic + 回复通道）。 |
| **可观测** | 可选 **`Observer`** 回调，库内**不**依赖 Prometheus，由业务自行对接指标或日志。 |

---

## 环境与依赖

- **Go** ≥ 1.21（以 `go.mod` 为准）。
- **Redis**：需支持 Stream 与 Sorted Set（常见 6.x/7.x 均可）。
- 业务必须在首次访问 Redis 前调用 **`RegisterRedisMqConfig`**，否则库会在取配置时 **panic**（提示未设置地址）。

---

## 安装

```bash
go get github.com/jackyang-hk/go-redismq@v1.2.2
```

具体版本请以 [CHANGELOG.md](./CHANGELOG.md) 与团队约定为准。

---

## 快速开始

包名为 **`go_redismq`**，建议使用别名导入：

```go
import redismq "github.com/jackyang-hk/go-redismq"
```

1. **进程启动时注册 Redis**（消费组名全局一致）：

```go
redismq.RegisterRedisMqConfig(&redismq.RedisMqConfig{
    Group:    "GID_YourApp",
    Addr:     "127.0.0.1:6379",
    Password: "",
    Database: 0,
})
```

2. **注册监听器**并 **启动消费**：

```go
redismq.RegisterListener(myListener{})
redismq.StartRedisMqConsumer()
```

3. **发送消息**：

```go
_, err := redismq.Send(&redismq.Message{
    Topic: "orders",
    Tag:   "created",
    Body:  `{"id":"123"}`,
})
```

4. 实现 **`IMessageListener`**：`GetTopic`、`GetTag`、`Consume`，返回 **`CommitMessage`** 或 **`ReconsumeLater`**。

---

## 配置说明

| 字段 | 含义 |
|------|------|
| `Group` | Redis **消费组名**，同一服务多实例应相同，以便负载均衡。 |
| `Addr` | Redis 地址 `host:port`。 |
| `Password` | Redis 密码；无则空字符串。 |
| `Database` | Redis DB 序号。 |

Stream 物理名称由 topic 与库内前缀拼接生成（如 `name.go` 中 `MQ_QUEUE_LIST_STREAM_<topic>_V3`）。不同应用若误用相同 `Group` 且 topic 重叠，会共享同一队列命名空间，需谨慎。

---

## 核心概念

- **`Message`**：包含 `Topic`、`Tag`、`Body`、消息 id、重试次数、延迟相关字段（`StartDeliverTime`、`NextRetryDelaySeconds`、`NextDeliverAt`）及 `CustomData`。
- **监听键**：`GetMessageKey(topic, tag)`，每个 `(topic, tag)` **只能注册一个**监听器。
- **消费结果**：`CommitMessage` 表示处理完成并 ack；`ReconsumeLater` 进入延迟重投（受 `ReconsumeMax` 与死亡队列策略约束）。

---

## 生产消息

| API | 场景 |
|-----|------|
| **`Send`** | 普通入队，对应 Stream `XADD`。 |
| **延迟发送** | 设置 `StartDeliverTime` 等，由库写入延迟调度（与 `SendDelay` 等路径配合）。 |
| **`SendTransaction`** | 事务型：先 prepare 半消息，业务回调决定 **提交** 或 **回滚** 再真正进入 Stream。 |

发送时库会设置 `SendTime`；`XADD` 前 `MessageId` 应为空，由服务端生成。

---

## 消费消息

- **`StartRedisMqConsumer`** 内启动后台循环：阻塞读 Stream，按 `topic`+`tag` 派发到对应 listener。
- 每条消息在独立 **goroutine** 中处理：可按 `ConsumerDelayMilliSeconds` 先 sleep，再调 `Consume`，最后根据返回值 ack 或重新入延迟队列。
- 对过期过久的历史消息，库内可按策略直接丢弃（见 `consumer.go`），避免无限堆积。

---

## 重试与延迟调度

当返回 **`ReconsumeLater`** 时：

1. `ReconsumeTimes` 自增。
2. 下次投递时间优先看 **`NextDeliverAt`**（仍为未来时刻），其次 **`NextRetryDelaySeconds`**，否则使用 **默认线性退避 + 抖动**，并限制在 **[1s, 24h]**。
3. 超过 **`ReconsumeMax`** 及内部上限后，可能进入 **死亡队列** Stream。

不含新字段的老消息仍按兼容逻辑解析，行为与旧版本一致。

---

## 事务型发送

**`SendTransaction`** 适用于「先占位、再本地决策」的流程：

1. Prepare：半消息写入 Redis，并进入事务准备队列。
2. 业务回调返回 `CommitTransaction` 或 `RollbackTransaction`（或未知状态由后续 checker 处理）。
3. Commit：写入目标 Stream 并清理半消息；Rollback：删除准备态数据。

若消息带 **`StartDeliverTime`（延迟消息）**，事务 API 会直接报错，不支持。

---

## Invoke（类 RPC）

**`RegisterInvoke`** 注册方法名；**`Invoke`** 发内部消息并等待回复通道，在同一 Redis 上完成同步式调用语义。详见 `invoke.go` 与 `test/invoke_test.go`。

---

## 可观测性（可选）

通过 **`SetObserver`** 注册全局 **`Observer`**（见 `observer.go`）：

- **`OnSend`**：在 `send_stream`、`send_delay` 以及事务各阶段（如 `txn_prepare`、`txn_exec`、`txn_commit`、`txn_rollback`）完成后触发，带耗时与错误。
- **`OnConsume`**：在业务 **`Consume` 返回后**触发，耗时为 **Consume 函数体内**时间（不含前置 sleep）；若 **`Consume` panic**，会再触发一次且 **`Panic=true`**（此时 `Action` 无意义）。

回调必须 **极轻量**；重逻辑请异步处理。未注册时仅多一次读锁判空，对业务路径影响可忽略。

---

## 测试

| 命令 | 说明 |
|------|------|
| `go test ./... -short` | 快速单测，根目录包多数 **不需要** Redis。 |
| `go test ./...` | 包含 **`test/`** 集成测试，需本地 Redis（地址与密码见测试代码，如 `127.0.0.1:6379`）。 |

---

## 变更日志

详见 **[CHANGELOG.md](./CHANGELOG.md)**。

---

## 许可证

以仓库根目录许可证文件或组织约定为准。
