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

	buffer.addLocked(reading)
}

func (buffer *liveBuffer) addIfNewer(reading SensorReading) bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	for index := len(buffer.readings) - 1; index >= 0; index-- {
		existing := buffer.readings[index]
		if existing.DeviceID != reading.DeviceID {
			continue
		}
		if existing.Timestamp >= reading.Timestamp {
			return false
		}
		break
	}

	buffer.addLocked(reading)
	return true
}

func (buffer *liveBuffer) addLocked(reading SensorReading) {
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

func (buffer *liveBuffer) latestForDevice(limit int, deviceID string) []SensorReading {
	if deviceID == "" {
		return buffer.latest(limit)
	}

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	if limit <= 0 {
		limit = len(buffer.readings)
	}

	output := make([]SensorReading, 0, limit)
	for index := len(buffer.readings) - 1; index >= 0 && len(output) < limit; index-- {
		if buffer.readings[index].DeviceID == deviceID {
			output = append(output, buffer.readings[index])
		}
	}

	for left, right := 0, len(output)-1; left < right; left, right = left+1, right-1 {
		output[left], output[right] = output[right], output[left]
	}
	return output
}
