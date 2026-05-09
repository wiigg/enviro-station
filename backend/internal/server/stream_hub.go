package server

import "sync"

type streamHub struct {
	mu          sync.RWMutex
	subscribers map[chan SensorReading]string
}

func newStreamHub() *streamHub {
	return &streamHub{subscribers: make(map[chan SensorReading]string)}
}

func (hub *streamHub) subscribe(deviceID string) (chan SensorReading, func()) {
	channel := make(chan SensorReading, 64)

	hub.mu.Lock()
	hub.subscribers[channel] = deviceID
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
	for subscriber, deviceID := range hub.subscribers {
		if deviceID == "" || deviceID == reading.DeviceID {
			subscribers = append(subscribers, subscriber)
		}
	}
	hub.mu.RUnlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- reading:
		default:
		}
	}
}

func (hub *streamHub) subscriberCount(deviceID string) int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	if deviceID == "" {
		return len(hub.subscribers)
	}

	count := 0
	for _, subscriberDeviceID := range hub.subscribers {
		if subscriberDeviceID == "" || subscriberDeviceID == deviceID {
			count++
		}
	}
	return count
}
