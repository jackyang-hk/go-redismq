# go-redismq

基于 Redis Stream 与延迟队列（`MQ_DELAY_QUEUE_SET`）的消息队列 Go 库。

## 版本说明

当前推荐版本：**v1.2.1**（见 [CHANGELOG.md](./CHANGELOG.md)）。

### v1.2.1 概要

| 类别 | 说明 |
|------|------|
| **自定义重试时间** | 可选字段 `NextDeliverAt`（绝对时间）、`NextRetryDelaySeconds`（相对秒数）；未设置时与旧版行为兼容。 |
| **默认重试** | 仍以约 `60 × ReconsumeTimes` 秒为基数，并叠加有上限随机抖动，降低重试对齐。 |
| **延迟队列** | 修复多实例下 `ZRem` 竞态导致的重复投递问题。 |
| **测试** | 增加单元测试；`test/` 包内需本地 Redis（见测试内配置）。 |

详细变更列表见 [CHANGELOG.md](./CHANGELOG.md)。

## 依赖

```go
require github.com/jackyang-hk/go-redismq v1.2.1
```

## 运行测试

- 根目录单元测试（无需 Redis）：`go test ./... -short`
- 含 `test/` 集成测试（需 Redis `127.0.0.1:6379` 等，见 `test/` 内配置）：`go test ./...`

## 文档

- [重试风暴治理方案（业务侧与库侧规划）](./docs/retry-storm-mitigation-plan.md)
- [Changelly 延迟与 redismq 问题记录](./docs/changelly-delay-and-redismq-issue.md)
