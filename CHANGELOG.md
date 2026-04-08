# Changelog

All notable changes to this project are documented in this file.

## [v1.2.2] - 2026-04-09

### 可观测性（可选）

- 新增 **`Observer` 接口** 与 **`SetObserver`**：业务进程可注册一次回调，用于指标或日志，**不**引入 Prometheus 等硬依赖。
- **`OnSend`**：在 `send_stream`（`Send`/`XADD`）、`send_delay`（`SendDelay`）、事务各阶段（`txn_prepare` / `txn_exec` / `txn_rollback` / `txn_commit` 等）完成后调用，含耗时与错误。
- **`OnConsume`**：在注册的 `Consume` 返回后调用（含单次调用耗时）；若 `Consume` panic，会带 `Panic=true` 再调用一次（`Action` 在 panic 时无意义）。

### 文档

- 主文档 **`README.md` 改为英文**；新增 **`README.zh-CN.md`** 中文版，补充功能说明与使用结构。

### 升级说明

- 建议业务在 `go.mod` 中依赖：`github.com/jackyang-hk/go-redismq v1.2.2`。
- 未注册 `Observer` 时行为与 v1.2.1 一致；注册后请注意回调内勿阻塞。

[v1.2.2]: https://github.com/jackyang-hk/go-redismq/compare/v1.2.1...v1.2.2

## [v1.2.1] - 2026-03-24

### 已完成（本版本改造范围）

- **自定义下一次重试时间（兼容旧消息）**
  - `Message` / `MessageMetaData` 新增可选字段：
    - `NextRetryDelaySeconds`：相对延迟（秒），表示从当前时刻起再延迟多久投递。
    - `NextDeliverAt`：绝对投递时间（Unix 秒），仅当 **大于当前时间** 时生效。
  - 重试调度优先级：`NextDeliverAt`（未来有效） > `NextRetryDelaySeconds`（>0） > 默认策略。
  - 未设置或为非法值时，行为与旧版一致（零值回退）。
  - 序列化：已写入 Stream `metadata`，老消息无此字段时解析不受影响。

- **默认重试间隔：线性基数 + 抖动**
  - 仍保持「约 `60 × ReconsumeTimes` 秒」的线性基数，在此基础上增加有上限的随机抖动，减轻多消息在同一时刻扎堆重试（`spread = min(10×n, 300)`，且 `spread` 至少为 10 秒）。
  - 单次最终延迟仍限制在 `[1s, 24h]`。

- **延迟队列并发：修复重复投递竞态**
  - `pollingCore` 中仅在 `ZRem` **实际删除 1 条** member 后才执行投递；`removed == 0` 时视为已被其他实例抢占，跳过投递。

- **测试**
  - 新增单元测试：metadata 兼容、重试延迟解析、延迟队列 `ZRem` 分发判定；原有 `test/` 集成测试仍可通过。

### 升级说明

- 建议业务在 `go.mod` 中依赖：`github.com/jackyang-hk/go-redismq v1.2.1`。
- 无需迁移历史 Redis 数据；已在延迟队列中的任务仍按原 score 到期投递，新逻辑作用于**后续** `ReconsumeLater` 调度。

[v1.2.1]: https://github.com/jackyang-hk/go-redismq/compare/v1.2.0...v1.2.1
