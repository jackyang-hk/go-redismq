**go-redismq** is a Go library for implementing distributed message queues using Redis Streams. It supports message production, consumption, delayed delivery, transactions, and method invocation patterns.

## Features

- Message queueing with Redis Streams
- Delayed message delivery
- Transactional message sending and checking
- Method invocation via messages
- Customizable message listeners and checkers

## Getting Started

### Installation

Add the module to your project:

```
go get github.com/jackyang-hk/go-redismq
```

### Basic Usage

1. **Configure RedisMQ:**

```go
import goredismq "github.com/jackyang-hk/go-redismq"

goredismq.RegisterRedisMqConfig(&goredismq.RedisMqConfig{
    Group:    "YourGroup",
    Addr:     "127.0.0.1:6379",
    Password: "",
    Database: 0,
})
```

2. **Register a Listener:**

```go
type MyListener struct{}

func (l MyListener) GetTopic() string { return "topic" }
func (l MyListener) GetTag() string   { return "tag" }
func (l MyListener) Consume(ctx context.Context, msg *goredismq.Message) goredismq.Action {
    // handle message
    return goredismq.CommitMessage
}

goredismq.RegisterListener(&MyListener{})
```

3. **Start Consumer:**

```go
goredismq.StartRedisMqConsumer()
```

4. **Send a Message:**

```go
goredismq.Send(&goredismq.Message{
    Topic: "topic",
    Tag:   "tag",
    Body:  "Hello, World!",
})
```

## Testing

Run unit tests:

```
go test ./test/...
```
