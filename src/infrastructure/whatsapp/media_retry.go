package whatsapp

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types/events"
)

func (d *DeviceInstance) RegisterPendingMediaRetry(messageID string) (<-chan *events.MediaRetry, func(), error) {
	trimmedID := strings.TrimSpace(messageID)
	if trimmedID == "" {
		return nil, nil, fmt.Errorf("message id is required")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.pendingMediaRetry == nil {
		d.pendingMediaRetry = make(map[string]chan *events.MediaRetry)
	}
	if _, exists := d.pendingMediaRetry[trimmedID]; exists {
		return nil, nil, fmt.Errorf("media retry already pending for message %s", trimmedID)
	}

	ch := make(chan *events.MediaRetry, 1)
	d.pendingMediaRetry[trimmedID] = ch

	cleanup := func() {
		d.mu.Lock()
		defer d.mu.Unlock()

		if current, ok := d.pendingMediaRetry[trimmedID]; ok && current == ch {
			delete(d.pendingMediaRetry, trimmedID)
		}
	}

	return ch, cleanup, nil
}

func (d *DeviceInstance) DeliverPendingMediaRetry(evt *events.MediaRetry) bool {
	if d == nil || evt == nil {
		return false
	}

	messageID := strings.TrimSpace(string(evt.MessageID))
	if messageID == "" {
		return false
	}

	d.mu.Lock()
	ch, ok := d.pendingMediaRetry[messageID]
	if ok {
		delete(d.pendingMediaRetry, messageID)
	}
	d.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case ch <- evt:
	default:
	}

	return true
}
