package go_redismq

import (
	"context"
	"testing"
)

// panickingObserver always panics; library must not propagate to callers of observeSend/observeConsume.
type panickingObserver struct{}

func (panickingObserver) OnSend(context.Context, SendEvent) {
	panic("observer OnSend")
}

func (panickingObserver) OnConsume(context.Context, ConsumeEvent) {
	panic("observer OnConsume")
}

func TestObserveSend_DoesNotPropagateObserverPanic(t *testing.T) {
	SetObserver(panickingObserver{})
	defer SetObserver(nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic leaked from observeSend: %v", r)
		}
	}()

	observeSend(context.Background(), SendEvent{Operation: "test"})
}

func TestObserveConsume_DoesNotPropagateObserverPanic(t *testing.T) {
	SetObserver(panickingObserver{})
	defer SetObserver(nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic leaked from observeConsume: %v", r)
		}
	}()

	observeConsume(context.Background(), ConsumeEvent{})
}
