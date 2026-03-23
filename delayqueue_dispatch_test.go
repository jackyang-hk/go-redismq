package go_redismq

import (
	"errors"
	"testing"
)

func TestResolveDelayQueueDispatchDecision(t *testing.T) {
	dispatch, duplicate, errText := resolveDelayQueueDispatchDecision(1, nil)
	if !dispatch || duplicate || errText != "" {
		t.Fatalf("removed=1 should dispatch, got dispatch=%v duplicate=%v errText=%q", dispatch, duplicate, errText)
	}

	dispatch, duplicate, errText = resolveDelayQueueDispatchDecision(0, nil)
	if dispatch || !duplicate || errText != "" {
		t.Fatalf("removed=0 should be duplicate, got dispatch=%v duplicate=%v errText=%q", dispatch, duplicate, errText)
	}

	dispatch, duplicate, errText = resolveDelayQueueDispatchDecision(0, errors.New("redis down"))
	if dispatch || duplicate || errText == "" {
		t.Fatalf("error case should return errText, got dispatch=%v duplicate=%v errText=%q", dispatch, duplicate, errText)
	}
}
