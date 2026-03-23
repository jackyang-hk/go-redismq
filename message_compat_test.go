package go_redismq

import (
	"testing"
)

func TestMessageMetadataRoundTripWithNewFields(t *testing.T) {
	msg := &Message{
		Topic:                 "topic_a",
		Tag:                   "tag_a",
		Body:                  "body_a",
		ReconsumeTimes:        3,
		ReconsumeMax:          99,
		NextRetryDelaySeconds: 120,
		NextDeliverAt:         1234567890,
	}

	args := msg.toStreamAddArgsValues("stream_a")
	values, ok := args.Values.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map values, got %T", args.Values)
	}
	parsed := &Message{}
	parsed.passStreamMessage(values)

	if parsed.NextRetryDelaySeconds != 120 {
		t.Fatalf("expected NextRetryDelaySeconds=120, got %d", parsed.NextRetryDelaySeconds)
	}
	if parsed.NextDeliverAt != 1234567890 {
		t.Fatalf("expected NextDeliverAt=1234567890, got %d", parsed.NextDeliverAt)
	}
}

func TestMessageMetadataRoundTripWithLegacyMetadata(t *testing.T) {
	legacyValues := map[string]interface{}{
		"topic": "legacy_topic",
		"tag":   "legacy_tag",
		"body":  "legacy_body",
		"metadata": `{"startDeliverTime":0,"reconsumeTimes":2,"reconsumeMax":10,"customData":{},"key":"k1","sendTime":1700000000000,"consumerDelayMilliSeconds":1000}`,
	}
	parsed := &Message{}
	parsed.passStreamMessage(legacyValues)

	if parsed.NextRetryDelaySeconds != 0 {
		t.Fatalf("expected legacy NextRetryDelaySeconds=0, got %d", parsed.NextRetryDelaySeconds)
	}
	if parsed.NextDeliverAt != 0 {
		t.Fatalf("expected legacy NextDeliverAt=0, got %d", parsed.NextDeliverAt)
	}
	if parsed.ReconsumeTimes != 2 {
		t.Fatalf("expected ReconsumeTimes=2, got %d", parsed.ReconsumeTimes)
	}
}
