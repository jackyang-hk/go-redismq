# Payment Checker 重试风暴治理方案

## 背景与问题

近期线上 `unibee-api` 出现 CPU/内存周期性波动。排查结果显示，`payment_checker` 消费链路存在明显重试风暴，导致网关调用和队列负载放大。

关键现象（只读排查得到）：

- `MQ_DELAY_QUEUE_SET` 长期在约 `9k` 规模波动，绝大多数为 `unibee_payment/payment_checker`。
- 延迟队列消息 `reconsumeTimes` 分布偏高，`P50` 已接近 `46`，`P90/P95/P99` 触顶到 `51`。
- 同一 `paymentId` 出现高频重复（单笔可达数百次）。
- 日志存在网关不可恢复/限流错误：
  - PayPal `404 INVALID_RESOURCE_ID`
  - PayPal/Stripe `429 rate limit`
- 代码中 `PaymentCheckerListener` 对多数错误统一 `ReconsumeLater`，导致异常单持续放大重试。

## 当前实现要点（代码定位）

- 消费入口：`internal/consumer/payment/payment_checker_listener.go`
  - `message.ReconsumeMax = 100`
  - `message.ReconsumeTimes > 50` 时 `Commit` 终止
  - 大部分网关异常路径直接 `ReconsumeLater`
- 首次入队：`internal/logic/payment/service/payment.go`
  - `GatewayPaymentCreate` 后发送 `TopicPaymentChecker`
- Topic 定义：`internal/cmd/redismq/redismq.go`
  - `TopicPaymentChecker = unibee_payment/payment_checker`

## 根因判断

不是单一内存泄漏，主要是以下叠加：

1. 异常分类过粗：不可恢复错误（如 404 资源不存在）仍被重试。
2. 重试节奏过密：失败后回投频率高，易形成自激。
3. 缺少同 `paymentId` 去重：并发消费者可能对同一单重复查询网关。
4. 缺少网关级限速保护：外部 429 后继续高频请求，进一步放大。

## 目标

- 降低无效重试比例（减少不可恢复错误重试）。
- 降低网关 429 比例和 `payment_checker` 流量尖峰。
- 稳定 `unibee-api` 的 CPU/内存波动。

## 治理方案（分阶段）

> 实施原则：**先做 `unibee-api` 侧可控优化，不改 `go-redismq` 公共库；库改造放最后阶段**。

### 阶段1：止血（建议先落地）

1. 错误分级与快速终止
  - 对明确不可恢复错误直接 `Commit`，不再 `ReconsumeLater`。
  - 典型规则：
    - PayPal `INVALID_RESOURCE_ID` / HTTP 404 -> 终止重试
    - 参数非法、资源不存在、签名非法等确定性错误 -> 终止重试
  - 已落地（`unibee-api`）：
    - PayPal 集成层（`internal/logic/gateway/api/paypal.go`）已对 `INVALID_RESOURCE_ID` / 404 做不可恢复识别。
    - `GatewayPaymentDetail` 命中后返回 `PaymentFailed` 结果并携带 `Reason/LastError`。
    - `payment_checker_listener` 走现有失败分支：回存 `last_error`、调用 `HandlePayFailure` 收敛、`Commit` 终止重试。
  - 健壮性说明（已校验）：
    - 使用 `*paypal.ErrorResponse` 安全类型断言并处理 nil 场景。
    - 同时覆盖 `HTTP 404` 与 `INVALID_RESOURCE_ID` 两类识别入口。
    - 对 `payment == nil` 做了保护，避免赋值金额/币种时空指针。
2. 网关限流错误退避升级
  - 对 429/5xx 使用指数退避 + 抖动，不使用固定短间隔重试。
  - 示例：`1m -> 2m -> 4m -> 8m ...`，上限可控（如 30m）。
  - 决策说明：
    - 不采用业务层“抬高 `ReconsumeTimes`”的临时技巧（可读性与可维护性较差）。
    - 该能力改为在 `go-redismq` 增加“指定下一次延期时间”后统一落地（见阶段4）。
    - 当前阶段（仅改 `unibee-api`）暂不实施 429 降频策略，避免引入临时性兼容代码。
3. 同 `paymentId` 并发去重
  - 消费前加短 TTL 互斥锁（Redis `SETNX`），锁键示例：
    - `lock:payment_checker:{paymentId}`
  - 拿不到锁则快速 `Commit` 或短延迟重试，避免同单并发打网关。
4. 网关调用速率保护
  - 按网关维度引入 client 侧 rate limiter（PayPal/Stripe 分开）。
  - 即使队列有积压，也不突破外部网关配额。

### 阶段2：短期优化

1. 动态重试策略
  - 按错误类型设置 `maxRetry` 与间隔模板，不再统一 `>50`。
  - 建议：
    - 不可恢复：`maxRetry=0`
    - 限流/临时错误：`maxRetry` 中等，间隔指数增长
    - 业务待收敛状态：低频巡检（分钟级）
2. 异常单隔离队列
  - 达到阈值（如 `reconsumeTimes >= 20`）迁移到低频队列或人工队列。
  - 主流程队列只保留高价值、可恢复消息。
3. 观测指标补齐
  - 按网关/错误码维度统计：
    - `receive`, `reconsume`, `commit_by_limit`, `rollback`
    - 429/404 占比
    - 单 `paymentId` 重试次数分布
  - 已落地（`unibee-api`）：
    - `payment_checker_listener` 新增结构化决策日志：
      - `payment_checker_decision paymentId:<id> reconsumeTimes:<n> decision:<commit|reconsume> reason:<...>`
    - 用于改造前后对比：
      - 不可恢复错误命中率
      - reconsume 主因分布
      - commit 终止原因分布

### 阶段3：中期治理

1. payment checker 状态机化
  - 显式定义 `可恢复/不可恢复/待人工` 状态，减少隐式分支。
2. 回查窗口业务化
  - “3天回查”可保留，但按状态动态降频，不应同频轮询全量单据。

### 阶段4：公共库治理（最后做）

1. 改造 `go-redismq` 重试策略能力
  - 支持可配置 backoff（线性/指数/自定义函数）。
  - 支持按错误类型动态决定重试间隔与最大重试次数。
  - 新增“指定下一次延期时间（Next Retry Delay / Next Deliver At）”能力，供 `payment_checker` 直接设置下一跳间隔。
  - 修复延迟队列并发竞态：`pollingCore` 从 `ZSet` 删除消息后，必须校验 `ZRem removed_count == 1` 才允许重新发送到 stream，避免多消费者并发轮询导致同一条消息重复投递。
2. 保留兼容模式
  - 默认保持旧行为，避免影响其他依赖该库的业务方。
3. 双轨验证
  - 先让 `payment_checker` 灰度使用新策略（含 429/5xx 降频重试），再逐步推广到其他 topic。
4. 本阶段与业务层关系
  - `paymentId` 去重锁作为可选兜底项，优先在 `go-redismq` 修复出队竞态后再评估是否仍需在 `unibee-api` 增加。

## 建议改造落点

- 主逻辑：
  - `internal/consumer/payment/payment_checker_listener.go`
- 入队/补偿：
  - `internal/logic/payment/service/payment.go`
  - `internal/logic/payment/service/split_payment.go`
- 统一错误分类（建议新增）：
  - `internal/logic/gateway/api/...` 下新增错误归类工具
- 观测与告警（建议新增）：
  - `payment_checker` 专属 metrics 与 dashboard

## 验收指标（上线后 24h 观察）

1. `MQ_DELAY_QUEUE_SET` 总量下降（目标：较当前下降 30% 以上）。
2. `reconsumeTimes P90` 降到 `< 20`。
3. 网关 429 占比下降（目标：下降 50% 以上）。
4. 单 `paymentId` 重试次数 TopN 显著下降。
5. `unibee-api` CPU/内存波动幅度收敛（峰值明显回落）。

## 风险与注意事项

- 错误分类一定要保守：仅对“确定不可恢复”错误快速终止，避免漏掉可恢复交易。
- 去重锁 TTL 不宜过长，避免真实重试被误抑制。
- 限速参数应按网关差异化，防止“一刀切”导致成功率下降。

## 执行建议

先做阶段1的 4 项止血改造，再做 24h 灰度观察；指标稳定后推进阶段2。  
阶段1~3 均在 `unibee-api` 内完成，不修改 `go-redismq`。  
公共库改造放到阶段4最后实施，避免扩大变更面。  
其中 429/5xx 的精细化退避能力明确延后到阶段4（`go-redismq` 改造）统一实现。  
如果需要，可再补一份“最小代码改动清单（按函数级别）”供直接实施。

## go-redismq 改造实施清单（可拷贝到新仓库）

> 适用目标：在不破坏历史行为的前提下，补齐“自定义重试延迟”能力，并修复延迟队列并发重复投递竞态。

### 一、改造目标

1. 支持业务方指定下一次重试延迟（而不是固定 `60 * reconsumeTimes`）。
2. 保持默认行为兼容（未设置新字段时行为不变）。
3. 修复延迟队列 `pollingCore` 并发下重复投递问题（`ZRem` 成功删除后才允许发送）。
4. 提供可验证的单元测试与最小灰度方案。

### 二、文件级改动清单（按优先级）

1. `message.go`（消息模型扩展）
  - 在消息结构新增可选字段（二选一或同时支持）：
    - `NextRetryDelaySeconds int64`（相对延迟，单位秒）
    - `NextDeliverAt int64`（绝对投递时间，Unix 秒）
  - 编解码处理：
    - 在 `toStreamAddArgsValues` / `passStreamMessage` 的 `metadata` 序列化与反序列化中加入新字段。
  - 兼容要求：
    - 字段为空/零值时，保持旧逻辑不变。
2. `consumer.go`（重试回投逻辑）
  - 在 `pushTaskToResumeLater` 计算 `StartDeliverTime` 时增加优先级：
    - 若 `NextDeliverAt > now`，优先使用 `NextDeliverAt`。
    - 否则若 `NextRetryDelaySeconds > 0`，使用 `now + NextRetryDelaySeconds`。
    - 否则走原有默认公式（保持兼容）。
  - 边界保护建议：
    - 最小值：`minDelaySeconds`（如 1s，避免 0/负数导致立即风暴）
    - 最大值：`maxDelaySeconds`（如 24h，避免异常值）
  - `ReconsumeTimes` 处理：
    - 继续按原语义递增，不因自定义延迟改变计数逻辑。
3. `delayqueue.go`（并发竞态修复）
  - 在 `pollingCore` 中，`ZRem` 后必须判断 `removed == 1` 才执行 `sendMessage`。
  - 若 `removed == 0`：
    - 说明该消息已被其他消费者抢先处理，本实例直接跳过。
  - 日志建议：
    - 打印 debug 级别“skip duplicate dispatch”日志，便于观测竞态命中频次。
4. `README.md` / `CHANGELOG.md`（文档）
  - 新增“自定义重试延迟”使用示例。
  - 说明默认兼容行为与新字段优先级规则。
  - 记录竞态修复影响面（预期仅减少重复投递）。

### 三、行为定义（避免歧义）

1. 字段优先级：`NextDeliverAt` > `NextRetryDelaySeconds` > 默认重试策略。
2. 字段清理时机：
  - 进入下一次消费后，业务可覆盖写入新值；库本身不强制清理，由最新消息体为准。
3. 非法值处理：
  - `NextDeliverAt <= now` 或 `NextRetryDelaySeconds <= 0` 视为未设置，回退默认策略。
4. 时钟基准：
  - 统一使用服务端当前时间（库内部 `now`），避免调用方传入本地时钟偏差导致抖动。

### 四、测试清单（必须覆盖）

1. `message` 编解码测试
  - 新字段写入 -> 反序列化后值一致。
  - 缺失新字段 -> 老消息正常解析。
2. `pushTaskToResumeLater` 逻辑测试
  - 仅有 `NextDeliverAt`：命中绝对时间分支。
  - 仅有 `NextRetryDelaySeconds`：命中相对延迟分支。
  - 两者都有：`NextDeliverAt` 优先。
  - 非法值：回退默认分支。
3. `pollingCore` 并发/幂等测试
  - 模拟两个消费者竞争同一 ZSet 消息：
    - 仅一个消费者 `removed == 1` 且成功 `sendMessage`。
    - 另一个 `removed == 0` 且不会 `sendMessage`。
4. 回归测试
  - 不设置新字段场景下，重试时间分布与旧版本一致。

### 五、灰度与发布建议

1. 版本建议：`v1.3.0`（新增能力 + 行为修复）。
2. 灰度顺序：
  - 先仅 `payment_checker` topic 使用新字段能力。
  - 观察 24h：`reconsumeTimes` 分布、重复投递计数、网关 429 比例。
  - 稳定后再推广其他 topic。
3. 回滚策略：
  - 业务侧停止写入新字段即可退回默认策略；
  - 必要时回退库版本到旧版（接口保持兼容，风险可控）。

### 六、业务接入示例（payment_checker）

1. 网关返回 429/5xx 时：
  - 设置 `NextRetryDelaySeconds` 为指数退避（含抖动），例如 `60, 120, 240, 480...`，上限 1800。
2. 明确不可恢复错误（如 PayPal 404/INVALID_RESOURCE_ID）：
  - 不再 `ReconsumeLater`，直接业务收敛并 `Commit`。
3. 普通可恢复错误：
  - 沿用默认策略或业务模板策略，不做激进重试。

## go-redismq v1.2.1 落地说明（本仓库已完成）

**标签**：`v1.2.1`（`CHANGELOG.md` / `README.md` 有完整说明）

**已实现能力**：

| 项 | 说明 |
|----|------|
| 自定义重试时间 | `Message` 支持可选 `NextDeliverAt`、`NextRetryDelaySeconds`（metadata 编解码；老消息兼容） |
| 调度优先级 | `NextDeliverAt`（未来有效）> `NextRetryDelaySeconds` > 默认策略；非法值回退默认 |
| 默认重试 | 保持约 `60 × ReconsumeTimes` 秒线性基数，并叠加有上限随机抖动，降低重试对齐 |
| 延迟队列竞态 | `pollingCore` 仅当 `ZRem` 删除条数为 1 时才投递，避免并发重复发送 |
| 测试 | 根包单测覆盖上述逻辑；`test/` 集成测试在本地 Redis 下可回归 |

**依赖升级**：业务工程 `go.mod` 使用 `github.com/jackyang-hk/go-redismq v1.2.1` 即可。

> 文档前文「阶段4 / 版本建议 v1.3.0」为早期规划；**本仓库实际发版为 v1.2.1**，能力与测试清单以 `CHANGELOG.md` 为准。后续若再发大版本可继续迭代。

