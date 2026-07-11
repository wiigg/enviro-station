package server

import (
	"sync"
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

func TestStreamHubPublishAndUnsubscribeConcurrently(t *testing.T) {
	const (
		iterations      = 50
		subscriberCount = 64
		publisherCount  = 8
		publishCount    = 32
	)

	for iteration := 0; iteration < iterations; iteration++ {
		hub := newStreamHub()
		unsubscribes := make([]func(), 0, subscriberCount)
		for subscriber := 0; subscriber < subscriberCount; subscriber++ {
			_, unsubscribe := hub.subscribe("")
			unsubscribes = append(unsubscribes, unsubscribe)
		}

		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		for _, unsubscribe := range unsubscribes {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				unsubscribe()
			}()
		}
		for publisher := 0; publisher < publisherCount; publisher++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				for reading := 0; reading < publishCount; reading++ {
					hub.publish(SensorReading{Timestamp: int64(reading)})
				}
			}()
		}

		close(start)
		waitGroup.Wait()

		if got := hub.subscriberCount(""); got != 0 {
			t.Fatalf("expected all subscribers to be removed, got %d", got)
		}
	}
}
