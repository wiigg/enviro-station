package server

import "sync"

type liveBuffer struct {
	mu       sync.RWMutex
	readings []SensorReading
	limit    int
}

func newLiveBuffer(limit int) *liveBuffer {
	if limit <= 0 {
		limit = 3600
	}

	return &liveBuffer{
		readings: make([]SensorReading, 0, limit),
		limit:    limit,
	}
}

func (buffer *liveBuffer) add(reading SensorReading) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	if len(buffer.readings) == buffer.limit {
		copy(buffer.readings, buffer.readings[1:])
		buffer.readings[len(buffer.readings)-1] = reading
		return
	}

	buffer.readings = append(buffer.readings, reading)
}

func (buffer *liveBuffer) latest(limit int) []SensorReading {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	if limit <= 0 || limit > len(buffer.readings) {
		limit = len(buffer.readings)
	}
	if limit == 0 {
		return []SensorReading{}
	}

	start := len(buffer.readings) - limit
	output := make([]SensorReading, limit)
	copy(output, buffer.readings[start:])
	return output
}
