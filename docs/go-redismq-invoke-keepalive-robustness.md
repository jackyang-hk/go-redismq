# go-redismq Invoke KeepAlive 健壮性问题

## 问题现象

调用方（如 unibee-api）通过 `go_redismq.Invoke` 请求 License 服务（Group: `GID_UniBee_License`）时，返回错误：

```
Invoke get group:redis: nil
```

**特征**：
- License 服务进程正常运行，HTTP OpenAPI 可访问，K8s 无异常
- License 服务侧**没有任何** GetMerchantLicense / GetLicenseByMerchantId 的调用日志
- **重启 License 服务后恢复正常**

---

## 根因分析

### Invoke 调用流程

1. 调用方执行 `Invoke(ctx, &InvoiceRequest{Group: "GID_UniBee_License", ...})`
2. go-redismq 首先在 Redis 中查询 `MessageInvokeGroup:GID_UniBee_License` 是否存在
3. 若 key 不存在（`redis.Nil`），**直接返回错误，不发送消息**
4. 若 key 存在，才将请求发送到 MQ，由 License 服务消费处理

因此，当 key 不存在时，消息根本不会到达 License 服务，故 License 侧无任何调用日志。

### KeepAlive 机制

License 服务（消费者）启动时会调用 `StartRedisMqConsumer()`，其中会启动 `keepAliveMessageInvokeListener` goroutine：

```go
// go-redismq/consumer.go
func StartRedisMqConsumer() {
    // ...
    go keepAliveMessageInvokeListener()
}
```

```go
// go-redismq/invoke_listener.go
func keepAliveMessageInvokeListener() {
    client := redis.NewClient(GetRedisConfig())
    defer client.Close()
    client.Set(context.Background(), fmt.Sprintf("MessageInvokeGroup:%s", Group), true, 300*time.Second)
    for {
        time.Sleep(60 * time.Second)
        client.Expire(context.Background(), fmt.Sprintf("MessageInvokeGroup:%s", Group), 300*time.Second)
    }
}
```

该 goroutine 负责：
- 首次 `Set` 写入 `MessageInvokeGroup:GID_UniBee_License`，TTL 300 秒
- 每 60 秒执行 `Expire` 续期 300 秒

### 问题本质

当 `keepAliveMessageInvokeListener` goroutine **异常退出**时：
- 主进程、HTTP 服务、其他 MQ 消费者继续运行，License 服务“看起来正常”
- 无人再续期 key，约 300 秒后 key 过期
- 调用方 Invoke 时查 key 得到 `redis.Nil`，返回 `Invoke get group:redis: nil`
- **重启 License 服务**会重新启动 keepAlive goroutine，重新写入 key，问题恢复

---

## go-redismq 库的健壮性缺陷

| 问题 | 说明 |
|------|------|
| **不检查错误** | `Set` / `Expire` 返回值被忽略，失败时无感知 |
| **无 panic 恢复** | goroutine 内发生 panic 会直接退出，主进程不受影响 |
| **单连接长期使用** | 一个 Redis client 贯穿整个生命周期，连接断开后无重连 |
| **无自愈能力** | goroutine 退出后不会自动重启 |

在 Redis 连接异常、网络抖动等情况下，keepAlive goroutine 可能静默退出，导致 Invoke 不可用。

---

## 改造建议（在 go-redismq 本仓库中）

go-redismq 为本仓库维护，可直接修改 `invoke_listener.go` 中的 `keepAliveMessageInvokeListener`，增强健壮性。

### 推荐方案：增强 keepAliveMessageInvokeListener

修改 `invoke_listener.go` 中的 `keepAliveMessageInvokeListener`：

```go
func keepAliveMessageInvokeListener() {
    key := fmt.Sprintf("MessageInvokeGroup:%s", Group)
    for {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    fmt.Printf("MQStream keepAliveMessageInvokeListener panic recovered: %v\n", r)
                }
            }()
            client := redis.NewClient(GetRedisConfig())
            defer client.Close()
            ctx := context.Background()
            if err := client.Set(ctx, key, true, 300*time.Second).Err(); err != nil {
                fmt.Printf("MQStream keepAliveMessageInvokeListener Set failed: %v\n", err)
                return
            }
            for {
                time.Sleep(60 * time.Second)
                if err := client.Expire(ctx, key, 300*time.Second).Err(); err != nil {
                    fmt.Printf("MQStream keepAliveMessageInvokeListener Expire failed: %v\n", err)
                    return // 退出内层循环，外层 for 会重新创建 client 重试
                }
            }
        }()
        time.Sleep(5 * time.Second) // 失败后短暂等待再重试
    }
}
```

**改造要点**：
- 增加 panic recover，避免 goroutine 因 panic 静默退出
- 检查 Set/Expire 错误并打日志，失败时可感知
- 失败时退出内层循环，外层 for 重新创建 client 并重试，具备自愈能力

### 备选方案：独立 KeepAlive 守护（在业务工程中）

若短期内无法发布 go-redismq 新版本，可在业务工程（如 unibee-license-api）启动时，额外启动一个独立的 goroutine 作为补充保障：

```go
// 在 main 或初始化逻辑中启动
go func() {
    key := "MessageInvokeGroup:GID_UniBee_License"
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        client := redis.NewClient(/* 使用与 redismq 相同的 Redis 配置 */)
        err := client.Set(context.Background(), key, true, 300*time.Second).Err()
        if err != nil {
            log.Errorf("KeepAlive backup Set failed: %v", err)
        }
        client.Close()
    }
}()
```

此方案仅作临时兜底，根本修复仍建议在本仓库中完成。

---

## 相关源码位置

- **invoke.go**：`client.Get("MessageInvokeGroup:"+req.Group)` 查 key，nil 时返回错误
- **invoke_listener.go**：`keepAliveMessageInvokeListener()` 维护 key
- **consumer.go**：`StartRedisMqConsumer()` 中 `go keepAliveMessageInvokeListener()`

---

## 参考

- 问题发生时间：2026-02-23 05:18:46
- 环境：unibee-api-daily (K8s)，merchantId: 15656
- 错误信息：`Invoke get group:redis: nil`
