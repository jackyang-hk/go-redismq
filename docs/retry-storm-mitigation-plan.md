# Payment Checker 重试风暴治理方案

> **给业务端（拷贝摘要）**  
> - **公共库 `go-redismq` 已发版 [`v1.2.1`](https://github.com/jackyang-hk/go-redismq/releases/tag/v1.2.1)**（本仓库 `CHANGELOG.md` / `README.md` 同步说明）。  
> - **已支持**：消息上可选 `NextDeliverAt` / `NextRetryDelaySeconds` 自定义下一次重试时间；默认仍为「约 `60×ReconsumeTimes` 秒」线性基数并带**有上限随机抖动**；延迟队列 **`ZRem` 幂等**修复（减少并发重复投递）。  
> - **业务侧动作**：在 `unibee-api`（等业务工程）`go.mod` 中将依赖升级为 `github.com/jackyang-hk/go-redismq v1.2.1`；按需于 `payment_checker` 等消费者在返回 `ReconsumeLater` 前设置上述字段（例如 429 场景）。**未设置新字段时行为与旧版兼容。**  
> - **仍在业务侧/后续迭代**：不可恢复错误快速 `Commit`、网关限流与去重、观测指标等见下文阶段 1～3。

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

> 实施原则（已更新）：**业务侧（`unibee-api` 等）止血与观测优先；公共库 `go-redismq` 中与重试/延迟队列相关的核心能力已在 `v1.2.1` 落地**，业务可升级依赖并按需使用新字段；其余库能力（可配置全局 backoff 策略等）仍可后续迭代。

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
  - **库能力（v1.2.1 已具备）**：`go-redismq` 已支持在 `Message` 上设置 `NextRetryDelaySeconds`（相对秒）或 `NextDeliverAt`（Unix 秒，须为未来时间）；业务可在识别 429/5xx 后写入再 `ReconsumeLater`，无需再依赖固定 `60*ReconsumeTimes`。
  - 决策说明：
    - 不采用业务层“抬高 `ReconsumeTimes`”的临时技巧（可读性与可维护性较差）。
    - **若暂不设置上述字段**，默认策略仍为线性基数 + 抖动（见 `v1.2.1` 说明）；**精细化 429 退避**建议在业务侧按错误类型写入 `NextRetryDelaySeconds`/`NextDeliverAt` 落地。
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

### 阶段4：公共库治理（部分已完成于 v1.2.1，其余可后续）

**已在 `go-redismq v1.2.1` 完成：**

- 新增「指定下一次延期时间」：`NextRetryDelaySeconds`、`NextDeliverAt`（metadata 序列化；老消息兼容）。
- 默认重试：未设置新字段时，仍以约 `60×ReconsumeTimes` 秒为线性基数，并叠加有上限随机抖动；单次延迟限制在 `[1s, 24h]`。
- 修复延迟队列并发竞态：`pollingCore` 仅在 `ZRem` 删除条数为 `1` 时才重新发送到 Stream，避免多实例重复投递。

**仍可后续迭代（未承诺版本）：**

- 库内全局可配置 backoff（线性/指数/自定义函数）、按错误类型自动选策略等。
- 业务侧双轨：可先让 `payment_checker` 灰度写入新字段（429/5xx 等），再推广到其他 topic。

**与业务层关系：**

- `paymentId` 去重锁仍为可选；出队竞态已在库侧缓解，是否叠加业务锁由业务评估。

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

1. **升级公共库**：业务工程依赖 **`github.com/jackyang-hk/go-redismq v1.2.1`**，发布前在测试环境验证消费、生产、延迟队列全链路。
2. **阶段1 止血**：继续做 `unibee-api` 侧错误分级、去重、限速与观测；其中 **429/5xx 退避** 已可在消费者侧结合 `NextRetryDelaySeconds` / `NextDeliverAt` 实现（库已支持）。
3. **灰度**：可先升级库仅验证默认行为（不写新字段），再对 `payment_checker` 等业务逐步写入新字段。
4. 若需要，可再补「最小代码改动清单（按函数级别）」供业务仓库实施。

## go-redismq 改造实施清单（与 v1.2.1 对照）

> **状态**：下列 **P0 项已在 `v1.2.1` 于本仓库实现并发布**；本清单保留作设计与验收对照。  
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
  - 不设置新字段场景下，仍以线性基数为主；`v1.2.1` 默认增加有上限抖动（总时长期望略增约数小时量级），用于打散重试对齐。

### 五、灰度与发布建议

1. **已发布版本**：`v1.2.1`（新增能力 + 行为修复 + 默认抖动）。
2. 灰度顺序（业务侧）：
  - 先升级依赖并验证**不写新字段**的兼容性；
  - 再仅对 `payment_checker` 等 topic **写入** `NextRetryDelaySeconds` / `NextDeliverAt`（如 429 场景）；
  - 观察 24h：`reconsumeTimes` 分布、重复投递计数、网关 429 比例。
3. 回滚策略：
  - 业务侧停止写入新字段即可退回默认策略；
  - 必要时回退依赖到 `v1.2.0` 及以前（未使用新字段时风险可控；使用新字段需与版本一致）。

### 六、业务接入示例（payment_checker）

1. **依赖**（业务工程 `go.mod`）：
   ```go
   require github.com/jackyang-hk/go-redismq v1.2.1
   ```
2. 网关返回 429/5xx 时（在返回 `ReconsumeLater` 前设置消息字段，由库计算下一跳投递）：
   - 设置 `NextRetryDelaySeconds` 为指数退避（含抖动），例如 `60, 120, 240, 480...`，上限 1800；或设置 `NextDeliverAt` 为 Unix 秒（须大于当前时间）。
3. 明确不可恢复错误（如 PayPal 404/INVALID_RESOURCE_ID）：
   - 不再 `ReconsumeLater`，直接业务收敛并 `Commit`。
4. 普通可恢复错误：
   - 不设置新字段则沿用库默认（线性基数 + 抖动）；需要更细策略时再写入 `NextRetryDelaySeconds` / `NextDeliverAt`。

## go-redismq v1.2.1 落地说明（本仓库已完成，与上文阶段4 / 清单一致）

| 项 | 说明 |
|----|------|
| **版本标签** | `v1.2.1`（详见仓库根目录 `CHANGELOG.md`、`README.md`） |
| **自定义重试时间** | `Message` 可选 `NextDeliverAt`、`NextRetryDelaySeconds`（写入 Stream `metadata`；老消息无字段则兼容） |
| **调度优先级** | `NextDeliverAt`（未来有效）> `NextRetryDelaySeconds` > 默认策略；非法值回退默认 |
| **默认重试** | 约 `60 × ReconsumeTimes` 秒线性基数 + 有上限随机抖动；单次延迟限制在 `[1s, 24h]` |
| **延迟队列** | `ZRem` 仅删除 1 条后才投递，减少多实例重复投递 |
| **测试** | 根包单测 + `test/` 包集成测试（需 Redis） |

**业务侧升级命令示例**：在业务仓库执行 `go get github.com/jackyang-hk/go-redismq@v1.2.1` 后联调发布。

