# go-redismq

[![Go Reference](https://pkg.go.dev/badge/github.com/jackyang-hk/go-redismq.svg)](https://pkg.go.dev/github.com/jackyang-hk/go-redismq)

A Go message-queue library built on **Redis Streams** (primary delivery) and a **sorted-set delay queue** (scheduled and re-delivery). It provides consumer groups, optional transactional produce, RPC-style invoke, and pluggable observability—without running a separate broker process.

**[简体中文](./README.zh-CN.md)**

---

## Table of contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Core model](#core-model)
- [Producing](#producing)
- [Consuming](#consuming)
- [Retries and delay scheduling](#retries-and-delay-scheduling)
- [Transactional produce](#transactional-produce)
- [Invoke (RPC-style)](#invoke-rpc-style)
- [Observability (optional)](#observability-optional)
  - [How to register](#how-to-register)
  - [`SendEvent.Operation` reference](#sendeventoperation-reference)
  - [`ConsumeEvent` fields](#consumeevent-fields)
  - [Example: metrics-friendly observer](#example-metrics-friendly-observer)
- [Testing](#testing)
- [Changelog](#changelog)

---

## Features

| Area | What you get |
|------|----------------|
| **Transport** | Messages stored in Redis Streams per topic; consumer groups for competing consumers. |
| **Routing** | Messages addressed by **`topic` + `tag`**; listeners register on that pair. |
| **Delay** | First-time delayed send via `StartDeliverTime`; re-delivery after `ReconsumeLater` uses an internal ZSET delay queue (`MQ_DELAY_QUEUE_SET`). |
| **Retries** | Configurable reconsume cap; default backoff uses a linear base (~`60 × ReconsumeTimes` seconds) plus bounded jitter; optional `NextDeliverAt` / `NextRetryDelaySeconds` on the message. |
| **Safety nets** | Death stream for messages that exceed retry limits; transactional half-messages with prepare/commit/rollback helpers. |
| **Invoke** | Request/reply over the same infrastructure (internal topic), for in-process style RPC. |
| **Telemetry** | Optional `Observer` hook for send/consume phases—no Prometheus dependency in the library. |

---

## Requirements

- **Go** ≥ 1.21 (see `go.mod`).
- **Redis** with Streams and sorted sets (Redis 5+; tested with recent 6.x/7.x).
- Application must call **`RegisterRedisMqConfig`** before any Redis operation (library panics if address is unset).

---

## Installation

```bash
go get github.com/jackyang-hk/go-redismq@v1.2.2
```

Use the latest tagged release that matches your `go.mod`. See [CHANGELOG.md](./CHANGELOG.md) for release notes.

---

## Quick start

The package name is `go_redismq`. A typical import alias:

```go
import redismq "github.com/jackyang-hk/go-redismq"
```

1. **Register Redis and consumer group name** (once at process startup):

```go
redismq.RegisterRedisMqConfig(&redismq.RedisMqConfig{
    Group:    "GID_YourApp",
    Addr:     "127.0.0.1:6379",
    Password: "",
    Database: 0,
})
```

2. **Register listeners** and **start the consumer loop**:

```go
redismq.RegisterListener(myListener{})
redismq.StartRedisMqConsumer()
```

3. **Send** messages from producers:

```go
_, err := redismq.Send(&redismq.Message{
    Topic: "orders",
    Tag:   "created",
    Body:  `{"id":"123"}`,
})
```

4. Implement **`IMessageListener`**: `GetTopic`, `GetTag`, and `Consume` returning `CommitMessage` or `ReconsumeLater`.

---

## Configuration

| Field | Meaning |
|-------|---------|
| `Group` | Redis **consumer group** name shared by all instances of your service. |
| `Addr` | `host:port` for Redis. |
| `Password` | Redis `AUTH` password (empty if none). |
| `Database` | Redis logical DB index. |

The library derives stream names from the topic (see `name.go` prefixes such as `MQ_QUEUE_LIST_STREAM_<topic>_V3`). Do not share the same `Group` string across unrelated applications unless you intend to share the same queue namespace.

---

## Core model

- **`Message`**: carries `Topic`, `Tag`, `Body`, ids, reconsume counters, optional delay fields (`StartDeliverTime`, `NextRetryDelaySeconds`, `NextDeliverAt`), and `CustomData`.
- **Listener key**: `GetMessageKey(topic, tag)`—each `(topic, tag)` pair maps to **one** registered listener.
- **Actions**: `CommitMessage` acknowledges processing; `ReconsumeLater` schedules another attempt via the delay queue (subject to `ReconsumeMax` and death-queue rules).

---

## Producing

| API | Use case |
|-----|----------|
| **`Send`** | Normal append to the topic stream (`XADD`). |
| **`SendDelay` / delayed `Send`** | When `StartDeliverTime` is set, the library routes through the delay mechanism (see producer helpers). |
| **`SendTransaction`** | Two-phase style: prepare half-message in Redis, run your local transaction callback, then commit or rollback the half-message to the stream. |

`SendTime` is set by the library on send. Empty `MessageId` is expected before `XADD` (server assigns id).

---

## Consuming

- Consumers run inside **`StartRedisMqConsumer`** (background goroutines): blocking read from streams, dispatch to the listener matching `topic`+`tag`.
- **Per-message goroutine**: after optional consumer-side delay (`ConsumerDelayMilliSeconds`), `Consume` is invoked; then ack or requeue depending on `Action`.
- Messages older than a configured age may be dropped (see `consumer.go`); this is library policy for stale backlog.

---

## Retries and delay scheduling

When you return **`ReconsumeLater`**:

1. `ReconsumeTimes` increments.
2. The next delay is derived from **`NextDeliverAt`** (if still in the future), else **`NextRetryDelaySeconds`**, else a **default** linear schedule (~`max(60, 60*n)` seconds) plus **jitter** (capped), clamped to `[1s, 24h]`.
3. After too many attempts (vs `ReconsumeMax` and internal caps), messages may move to a **death** stream.

Older messages without new metadata fields continue to behave as in previous releases.

---

## Transactional produce

**`SendTransaction`** is for workflows where you need a prepare step in Redis and a local decision:

1. Prepare: half-message stored + listed in a transaction prepare queue.
2. Your function returns `CommitTransaction` or `RollbackTransaction` (or unknown status for server-side checkers).
3. Commit path calls `XADD` and deletes the half-message; rollback deletes prepare state.

If you use delay messages (`StartDeliverTime`), transactional send is rejected by the API.

---

## Invoke (RPC-style)

**`RegisterInvoke`** registers a named handler; **`Invoke`** sends an internal message and waits for a reply channel. Used for synchronous-style calls over the same Redis infrastructure. See `invoke.go` and tests under `test/invoke_test.go`.

---

## Observability (optional)

The library exposes **`Observer`** and **`SetObserver`** in `observer.go`. Use them to drive **Prometheus**, **OpenTelemetry**, structured logs, or tracing—**without** the library importing any of those frameworks.

### How to register

1. Implement **`Observer`** with `OnSend` and `OnConsume`.
2. Call **`SetObserver(yourObserver)` once** during process startup (e.g. next to where you configure logging or metrics).  
   - You may register **before** or **after** `RegisterRedisMqConfig`; it does not use Redis.  
   - You should register **before** `StartRedisMqConsumer` if you want the first consumed messages to be observed.  
3. Call **`SetObserver(nil)`** to disable callbacks (e.g. in tests).

`SetObserver` replaces any previous observer; only **one** observer is active at a time.

### `SendEvent.Operation` reference

Use `ev.Operation` as a stable label for dashboards. Typical values:

| `Operation` | When it fires |
|---------------|----------------|
| `send_stream` | After a normal **`Send`** (`XADD` to the topic stream). `Source` is usually `ProducerWrapper`. |
| `send_delay` | After **`SendDelay`** (ZADD to the delay queue). |
| `txn_abort_validation` | Transaction aborted early (e.g. blank tag, or delay message not allowed). |
| `txn_prepare` | After the transactional **prepare** step to Redis. |
| `txn_exec` | After **your** `transactionExecuter` callback returns (check `ev.Err` and your returned status separately in app code if needed). |
| `txn_rollback` | After rolling back the half-message when status is rollback. |
| `txn_commit` | After committing the half-message to the stream. |
| `txn_unknown_status` | Unknown `TransactionStatus` from the callback. |

Each event includes **`Duration`** for that step, **`Success`**, and **`Err`** (may be `nil`).

### `ConsumeEvent` fields

| Field | Meaning |
|-------|---------|
| `Topic`, `Tag`, `MessageKey` | Routing identity (`MessageKey` = `topic_tag` style key used internally). |
| `MessageId` | Redis Stream message id. |
| `ReconsumeTimes` | Retry count **before** this delivery attempt. |
| `Action` | What your listener returned (`CommitMessage` / `ReconsumeLater`). Meaningless when **`Panic==true`**. |
| `Duration` | Time spent **inside** `Consume` only (excludes consumer-side sleep before `Consume`). |
| `Panic`, `PanicValue` | Set if `Consume` panicked; you still get one `OnConsume` for the panic path. |

### Example: metrics-friendly observer

Keep **`OnSend` / `OnConsume` O(1)**. Record counters or push to a channel; do **not** block on network I/O here.

```go
import (
    "context"
    redismq "github.com/jackyang-hk/go-redismq"
)

type metricsObserver struct{}

func (metricsObserver) OnSend(ctx context.Context, ev redismq.SendEvent) {
    // Example: increment outcome by operation + success
    // metrics.RedisMQSendTotal.WithLabelValues(ev.Operation, strconv.FormatBool(ev.Success)).Inc()
    // metrics.RedisMQSendSeconds.WithLabelValues(ev.Operation).Observe(ev.Duration.Seconds())
    _ = ctx
}

func (metricsObserver) OnConsume(ctx context.Context, ev redismq.ConsumeEvent) {
    // Example: histogram of handler latency; separate label for panic
    // metrics.RedisMQConsumeSeconds.WithLabelValues(ev.Topic, ev.Tag, strconv.FormatBool(ev.Panic)).Observe(ev.Duration.Seconds())
    _ = ctx
}

// In main / init (before StartRedisMqConsumer if you need first messages observed):
// redismq.SetObserver(metricsObserver{})
```

If you must do slow work, **spawn a short-lived goroutine** or hand off to a worker pool; never block the MQ hot path.

When no observer is set, cost is one read lock and a nil check per callback site.

---

## Testing

| Command | Scope |
|---------|--------|
| `go test ./... -short` | Fast unit tests (no Redis required for most root-package tests). |
| `go test ./...` | Includes `test/` integration tests; requires Redis (see `test/*_test.go` for `Addr` / password). |

Example integration config in tests: `127.0.0.1:6379` with password as configured locally.

---

## Changelog

Release history and migration notes: **[CHANGELOG.md](./CHANGELOG.md)**.

---

## License

See the license file in the repository root (if present) or your organization’s default for `github.com/jackyang-hk/go-redismq`.
