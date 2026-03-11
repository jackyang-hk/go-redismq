package test

import (
	"context"
	"github.com/gogf/gf/v2/frame/g"
	goredismq "github.com/jackyang-hk/go-redismq"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

var receiveCount = 0

type TestListener struct {
	// ReconsumeOnce makes the first Consume return ReconsumeLater so the message goes through the delay queue.
	ReconsumeOnce bool
}

func (t TestListener) GetTopic() string {
	return "test"
}

func (t TestListener) GetTag() string {
	return "test"
}

func (t TestListener) Consume(ctx context.Context, message *goredismq.Message) goredismq.Action {
	receiveCount = receiveCount + 1
	g.Log().Infof(ctx, "Receive Message %d:%s", receiveCount, goredismq.MarshalToJsonString(message))
	if t.ReconsumeOnce && receiveCount == 1 {
		return goredismq.ReconsumeLater
	}
	return goredismq.CommitMessage
}

func TestProducerAndConsumer(t *testing.T) {
	goredismq.RegisterRedisMqConfig(&goredismq.RedisMqConfig{
		Group:    TestGroup,
		Addr:     "127.0.0.1:6379",
		Password: "changeme",
		Database: 0,
	})
	goredismq.RegisterListener(&TestListener{})
	goredismq.StartRedisMqConsumer()
	t.Run("Test Start RedisMQ", func(t *testing.T) {
		go func() {
			for {
				result, err := goredismq.Send(&goredismq.Message{
					Topic: "test",
					Tag:   "test",
					Body:  "Test",
				})
				require.Nil(t, err, "error")
				require.Equal(t, result, true)
				time.Sleep(1 * time.Second)
			}
		}()

		time.Sleep(5 * time.Second)
		require.Equal(t, receiveCount > 0, true)
	})
}

// TestDelayQueueReconsume verifies that when Consume returns ReconsumeLater, the message is put in the
// delay queue and redelivered after the delay (first reconsume is 60s). Skips in short mode (e.g. -short).
func TestDelayQueueReconsume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping delay queue reconsume test in short mode (waits ~70s)")
	}
	receiveCount = 0
	goredismq.RegisterRedisMqConfig(&goredismq.RedisMqConfig{
		Group:    TestGroup,
		Addr:     "127.0.0.1:6379",
		Password: "changeme",
		Database: 0,
	})
	goredismq.RegisterListener(&TestListener{ReconsumeOnce: true})
	goredismq.StartRedisMqConsumer()

	_, err := goredismq.Send(&goredismq.Message{
		Topic: "test",
		Tag:   "test",
		Body:  "DelayQueueTest",
	})
	require.Nil(t, err)

	// First delivery: receiveCount becomes 1, we return ReconsumeLater -> message goes to delay queue (60s).
	// Wait for delay queue to redeliver: 60s + poll interval buffer.
	time.Sleep(70 * time.Second)
	require.GreaterOrEqual(t, receiveCount, 2, "message should be received at least twice (first + redelivery from delay queue)")
}
