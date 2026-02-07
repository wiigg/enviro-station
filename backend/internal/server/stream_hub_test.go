package server

import (
	"testing"
	"time"
)

func TestStreamHubPublishDeliversReading(t *testing.T) {
	hub := newStreamHub()
	channel, unsubscribe := hub.subscribe()
	defer unsubscribe()

	reading := SensorReading{Timestamp: 1738886400, Temperature: 22.4}
	hub.publish(reading)

	select {
	case received := <-channel:
		if received.Timestamp != reading.Timestamp {
			t.Fatalf("expected timestamp %d, got %d", reading.Timestamp, received.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("expected published reading")
	}
}
