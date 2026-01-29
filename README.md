# GO-REDISMQ

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

# Logger Configuration Guide

The library uses a flexible logger interface that allows you to inject your own logger implementation.

## Quick Start

### 1. Default Colored Logger (Recommended for Development)

No setup needed! Just use the library and it will automatically use a colored slog logger:

```go
package main

import "github.com/Orfeo42/go-redismq"

func main() {
    // That's it! Uses colored output by default
    go_redismq.StartRedisMqConsumer()
}
```

**Control log level with environment variable:**

```bash
export LOG_LEVEL=DEBUG    # Shows all logs (DEBUG, INFO, WARN, ERROR)
export LOG_LEVEL=INFO     # Default - shows INFO, WARN, ERROR
export LOG_LEVEL=WARN     # Shows only WARN and ERROR
export LOG_LEVEL=ERROR    # Shows only ERROR
```

**Output format:**

```
[INFO] 2024-01-29 15:04:05 [thread-1] MQStream Start Delay Queue!
[ERROR] 2024-01-29 15:04:05 [thread-1] [consumer.go:124] MQStream Error...
```

---

## Production Setups

### 2. JSON Structured Logging (Recommended for Production)

```go
package main

import (
    "log/slog"
    "os"
    "github.com/Orfeo42/go-redismq"
)

func main() {
    // Create JSON logger
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level:     slog.LevelInfo,
        AddSource: false, // Set to true to include source code location
    }))

    go_redismq.SetSlogLogger(logger)
    go_redismq.StartRedisMqConsumer()
}
```

**Output:**

```json
{
  "time": "2024-01-29T15:04:05",
  "level": "INFO",
  "msg": "MQStream Start Delay Queue!"
}
```

---

### 3. Log to File

```go
package main

import (
    "log/slog"
    "os"
    "github.com/Orfeo42/go-redismq"
)

func main() {
    // Open log file
    logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    if err != nil {
        panic(err)
    }
    defer logFile.Close()

    // Create logger writing to file
    logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))

    go_redismq.SetSlogLogger(logger)
    go_redismq.StartRedisMqConsumer()
}
```

---

### 4. Standard Go Logger

```go
package main

import (
    "log"
    "os"
    "github.com/Orfeo42/go-redismq"
)

func main() {
    logger := log.New(os.Stdout, "[RedisMQ] ", log.LstdFlags)
    go_redismq.SetStdLogger(logger)

    go_redismq.StartRedisMqConsumer()
}
```

---

## Third-Party Logger Integration

### 5. Logrus

```go
package main

import (
    "github.com/sirupsen/logrus"
    "github.com/Orfeo42/go-redismq"
)

// Create adapter
type LogrusAdapter struct {
    logger *logrus.Logger
}

func (l *LogrusAdapter) Debugf(format string, args ...any) {
    l.logger.Debugf(format, args...)
}

func (l *LogrusAdapter) Infof(format string, args ...any) {
    l.logger.Infof(format, args...)
}

func (l *LogrusAdapter) Warnf(format string, args ...any) {
    l.logger.Warnf(format, args...)
}

func (l *LogrusAdapter) Errorf(format string, args ...any) {
    l.logger.Errorf(format, args...)
}

func main() {
    // Setup logrus
    logrusLogger := logrus.New()
    logrusLogger.SetLevel(logrus.InfoLevel)
    logrusLogger.SetFormatter(&logrus.JSONFormatter{})

    // Use adapter
    go_redismq.SetLogger(&LogrusAdapter{logger: logrusLogger})
    go_redismq.StartRedisMqConsumer()
}
```

---

### 6. Zap

```go
package main

import (
    "go.uber.org/zap"
    "github.com/Orfeo42/go-redismq"
)

// Create adapter
type ZapAdapter struct {
    sugar *zap.SugaredLogger
}

func (l *ZapAdapter) Debugf(format string, args ...any) {
    l.sugar.Debugf(format, args...)
}

func (l *ZapAdapter) Infof(format string, args ...any) {
    l.sugar.Infof(format, args...)
}

func (l *ZapAdapter) Warnf(format string, args ...any) {
    l.sugar.Warnf(format, args...)
}

func (l *ZapAdapter) Errorf(format string, args ...any) {
    l.sugar.Errorf(format, args...)
}

func main() {
    // Setup zap
    zapLogger, _ := zap.NewProduction()
    defer zapLogger.Sync()

    // Use adapter
    go_redismq.SetLogger(&ZapAdapter{sugar: zapLogger.Sugar()})
    go_redismq.StartRedisMqConsumer()
}
```

---

### 7. Zerolog

```go
package main

import (
    "os"
    "github.com/rs/zerolog"
    "github.com/Orfeo42/go-redismq"
)

// Create adapter
type ZerologAdapter struct {
    logger zerolog.Logger
}

func (l *ZerologAdapter) Debugf(format string, args ...any) {
    l.logger.Debug().Msgf(format, args...)
}

func (l *ZerologAdapter) Infof(format string, args ...any) {
    l.logger.Info().Msgf(format, args...)
}

func (l *ZerologAdapter) Warnf(format string, args ...any) {
    l.logger.Warn().Msgf(format, args...)
}

func (l *ZerologAdapter) Errorf(format string, args ...any) {
    l.logger.Error().Msgf(format, args...)
}

func main() {
    // Setup zerolog
    zerologLogger := zerolog.New(os.Stdout).With().Timestamp().Logger()

    // Use adapter
    go_redismq.SetLogger(&ZerologAdapter{logger: zerologLogger})
    go_redismq.StartRedisMqConsumer()
}
```

---

## Logger Interface

To implement your own logger, satisfy this interface:

```go
type Logger interface {
    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
}
```

**Example:**

```go
type MyLogger struct{}

func (l *MyLogger) Debugf(format string, args ...any) {
    // Your implementation
}

func (l *MyLogger) Infof(format string, args ...any) {
    // Your implementation
}

func (l *MyLogger) Warnf(format string, args ...any) {
    // Your implementation
}

func (l *MyLogger) Errorf(format string, args ...any) {
    // Your implementation
}

// Use it
go_redismq.SetLogger(&MyLogger{})
```

---

## Tips

1. **Development**: Use default colored logger with `LOG_LEVEL=DEBUG`
2. **Production**: Use JSON structured logging for better log aggregation
3. **File Logging**: Remember to handle log rotation (use tools like logrotate)
4. **Performance**: If high performance is critical, use Zap or Zerolog
5. **Integration**: If you already use a logger in your app, create an adapter

---

## Environment Variables

- `LOG_LEVEL`: Controls the default logger level (DEBUG, INFO, WARN, ERROR)
  - Default: INFO
  - Example: `export LOG_LEVEL=DEBUG`

---

## API Functions

```go
// Set custom logger implementing the Logger interface
func SetLogger(l Logger)

// Set Go's standard log.Logger
func SetStdLogger(l *log.Logger)

// Set Go's slog.Logger
func SetSlogLogger(l *slog.Logger)

// Get current logger
func GetLogger() Logger
```
