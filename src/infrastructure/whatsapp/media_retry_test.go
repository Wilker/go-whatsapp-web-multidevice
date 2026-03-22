package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestRegisterPendingMediaRetryDeliversEvent(t *testing.T) {
	instance := NewDeviceInstance("device-1", nil, nil)

	retryCh, cleanup, err := instance.RegisterPendingMediaRetry("msg-1")
	if err != nil {
		t.Fatalf("RegisterPendingMediaRetry() unexpected error: %v", err)
	}
	defer cleanup()

	evt := &events.MediaRetry{MessageID: types.MessageID("msg-1")}
	if delivered := instance.DeliverPendingMediaRetry(evt); !delivered {
		t.Fatal("expected media retry event to be delivered")
	}

	select {
	case received := <-retryCh:
		if received != evt {
			t.Fatal("expected the original event pointer to be delivered")
		}
	default:
		t.Fatal("expected retry channel to receive an event")
	}
}

func TestDeliverPendingMediaRetryReturnsFalseForUnknownMessage(t *testing.T) {
	instance := NewDeviceInstance("device-1", nil, nil)

	if delivered := instance.DeliverPendingMediaRetry(&events.MediaRetry{MessageID: types.MessageID("missing")}); delivered {
		t.Fatal("expected delivery to fail when there is no pending waiter")
	}
}
