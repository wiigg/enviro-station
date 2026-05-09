package server

import (
	"testing"
	"time"
)

func TestStreamHubPublishDeliversReading(t *testing.T) {
	hub := newStreamHub()
	channel, unsubscribe := hub.subscribe("")
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

func TestStreamHubPublishFiltersByDevice(t *testing.T) {
	hub := newStreamHub()
	channel, unsubscribe := hub.subscribe("pi-2")
	defer unsubscribe()

	hub.publish(SensorReading{DeviceID: "pi-1", Timestamp: 1738886400})
	hub.publish(SensorReading{DeviceID: "pi-2", Timestamp: 1738886401})

	select {
	case received := <-channel:
		if received.DeviceID != "pi-2" {
			t.Fatalf("expected pi-2 reading, got %q", received.DeviceID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected published reading")
	}
}

func TestStreamHubSubscriberCountIncludesUnfilteredSubscribers(t *testing.T) {
	hub := newStreamHub()
	_, unsubscribeAll := hub.subscribe("")
	defer unsubscribeAll()
	_, unsubscribeDevice := hub.subscribe("pi-1")
	defer unsubscribeDevice()

	if got := hub.subscriberCount("pi-1"); got != 2 {
		t.Fatalf("expected 2 subscribers for pi-1, got %d", got)
	}
	if got := hub.subscriberCount("pi-2"); got != 1 {
		t.Fatalf("expected 1 subscriber for pi-2, got %d", got)
	}
}
