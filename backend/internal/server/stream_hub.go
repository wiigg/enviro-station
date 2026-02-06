package server

import "sync"

type streamHub struct {
	mu          sync.RWMutex
	subscribers map[chan SensorReading]struct{}
}

func newStreamHub() *streamHub {
	return &streamHub{subscribers: make(map[chan SensorReading]struct{})}
}

func (hub *streamHub) subscribe() (chan SensorReading, func()) {
	channel := make(chan SensorReading, 64)

	hub.mu.Lock()
	hub.subscribers[channel] = struct{}{}
	hub.mu.Unlock()

	unsubscribe := func() {
		hub.mu.Lock()
		if _, exists := hub.subscribers[channel]; exists {
			delete(hub.subscribers, channel)
			close(channel)
		}
		hub.mu.Unlock()
	}

	return channel, unsubscribe
}

func (hub *streamHub) publish(reading SensorReading) {
	hub.mu.RLock()
	subscribers := make([]chan SensorReading, 0, len(hub.subscribers))
	for subscriber := range hub.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	hub.mu.RUnlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- reading:
		default:
		}
	}
}
