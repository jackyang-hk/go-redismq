package go_redismq

import (
	"context"
	"sync"
	"time"
)

// Observer receives optional telemetry callbacks for producer and consumer paths.
// Implementations must be lightweight; defer heavy work to another goroutine if needed.
type Observer interface {
	// OnSend is invoked after a producer-side step completes (stream XADD, delay ZADD, transaction phases).
	OnSend(ctx context.Context, ev SendEvent)
	// OnConsume is invoked after the registered listener's Consume returns, or when Consume panics.
	OnConsume(ctx context.Context, ev ConsumeEvent)
}

// SendEvent describes one producer-side observation. Operation distinguishes the step
// (e.g. send_stream, txn_prepare, txn_exec).
type SendEvent struct {
	Operation string
	Topic     string
	Tag       string
	Source    string // e.g. ProducerWrapper for Send(); may be empty for internal steps
	Success   bool
	Err       error
	Duration  time.Duration
}

// ConsumeEvent describes one consumer dispatch (single listener invocation).
type ConsumeEvent struct {
	Topic          string
	Tag            string
	MessageKey     string
	MessageId      string
	ReconsumeTimes int
	Action         Action
	Duration       time.Duration
	Panic          bool
	PanicValue     interface{}
}

var observerMu sync.RWMutex
var observer Observer

// SetObserver registers a process-wide observer. Pass nil to disable callbacks.
func SetObserver(o Observer) {
	observerMu.Lock()
	defer observerMu.Unlock()
	observer = o
}

func getObserver() Observer {
	observerMu.RLock()
	defer observerMu.RUnlock()
	return observer
}

func observeSend(ctx context.Context, ev SendEvent) {
	o := getObserver()
	if o == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		recover() // do not let Observer.OnSend panic affect producer return path
	}()
	o.OnSend(ctx, ev)
}

func observeConsume(ctx context.Context, ev ConsumeEvent) {
	o := getObserver()
	if o == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		recover() // do not let Observer.OnConsume panic skip ack/requeue after Consume
	}()
	o.OnConsume(ctx, ev)
}
