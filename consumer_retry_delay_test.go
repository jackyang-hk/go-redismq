package go_redismq

import "testing"

func TestResolveNextDelaySeconds_Priority(t *testing.T) {
	now := int64(1000)

	msg := &Message{
		ReconsumeTimes:        5,
		NextRetryDelaySeconds: 120,
		NextDeliverAt:         now + 300,
	}
	delay := resolveNextDelaySeconds(msg, now)
	if delay != 300 {
		t.Fatalf("expected delay=300 from NextDeliverAt, got %d", delay)
	}
}

func TestResolveNextDelaySeconds_UseRelativeWhenNoAbsolute(t *testing.T) {
	now := int64(1000)
	msg := &Message{
		ReconsumeTimes:        5,
		NextRetryDelaySeconds: 120,
		NextDeliverAt:         now,
	}
	delay := resolveNextDelaySeconds(msg, now)
	if delay != 120 {
		t.Fatalf("expected delay=120 from NextRetryDelaySeconds, got %d", delay)
	}
}

func TestResolveNextDelaySeconds_FallbackAndBounds(t *testing.T) {
	now := int64(1000)
	msgDefault := &Message{ReconsumeTimes: 2}
	delayDefault := resolveNextDelaySeconds(msgDefault, now)
	// base=120, spread=20 → [120, 140]
	if delayDefault < 120 || delayDefault > 140 {
		t.Fatalf("expected default delay in [120,140] (linear base + jitter), got %d", delayDefault)
	}

	msgMin := &Message{ReconsumeTimes: 1, NextRetryDelaySeconds: -10}
	delayMin := resolveNextDelaySeconds(msgMin, now)
	// invalid NextRetryDelaySeconds ignored → default: base=60, spread=10 → [60, 70]
	if delayMin < 60 || delayMin > 70 {
		t.Fatalf("expected fallback delay in [60,70] for invalid relative value, got %d", delayMin)
	}

	msgMax := &Message{ReconsumeTimes: 1, NextRetryDelaySeconds: 999999}
	delayMax := resolveNextDelaySeconds(msgMax, now)
	if delayMax != 24*60*60 {
		t.Fatalf("expected max bounded delay=%d, got %d", 24*60*60, delayMax)
	}
}
