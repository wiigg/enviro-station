package server

import "sync"

type opsEventBuffer struct {
	mu     sync.RWMutex
	events []OpsEvent
	limit  int
}

func newOpsEventBuffer(limit int) *opsEventBuffer {
	if limit <= 0 {
		limit = maxOpsEventsLimit
	}

	return &opsEventBuffer{
		events: make([]OpsEvent, 0, limit),
		limit:  limit,
	}
}

func (buffer *opsEventBuffer) add(event OpsEvent) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	if len(buffer.events) == buffer.limit {
		copy(buffer.events, buffer.events[1:])
		buffer.events[len(buffer.events)-1] = event
		return
	}

	buffer.events = append(buffer.events, event)
}

func (buffer *opsEventBuffer) latest(limit int) []OpsEvent {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	if limit <= 0 || limit > len(buffer.events) {
		limit = len(buffer.events)
	}
	if limit == 0 {
		return []OpsEvent{}
	}

	output := make([]OpsEvent, 0, limit)
	for index := len(buffer.events) - 1; index >= 0 && len(output) < limit; index-- {
		output = append(output, buffer.events[index])
	}

	return output
}
