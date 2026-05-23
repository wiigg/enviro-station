package server

import (
	"testing"
	"time"
)

func TestPublishLiveSkipsStaleReadingForDevice(t *testing.T) {
	api := NewAPI(&fakeStore{}, "secret")
	channel, unsubscribe := api.stream.subscribe("pi-1")
	defer unsubscribe()

	api.publishLive(SensorReading{DeviceID: "pi-1", Timestamp: 200, PM2: 4})
	received := receiveLiveReading(t, channel)
	if received.Timestamp != 200 {
		t.Fatalf("expected first live timestamp 200, got %d", received.Timestamp)
	}

	api.publishLive(SensorReading{DeviceID: "pi-1", Timestamp: 100, PM2: 12})

	select {
	case stale := <-channel:
		t.Fatalf("expected stale reading to be skipped, got timestamp %d", stale.Timestamp)
	case <-time.After(50 * time.Millisecond):
	}

	readings := api.live.latestForDevice(10, "pi-1")
	if len(readings) != 1 {
		t.Fatalf("expected one buffered reading, got %d", len(readings))
	}
	if readings[0].Timestamp != 200 {
		t.Fatalf("expected buffered timestamp 200, got %d", readings[0].Timestamp)
	}
}

func TestPublishLiveAllowsOlderReadingForDifferentDevice(t *testing.T) {
	api := NewAPI(&fakeStore{}, "secret")
	channel, unsubscribe := api.stream.subscribe("")
	defer unsubscribe()

	api.publishLive(SensorReading{DeviceID: "pi-1", Timestamp: 200, PM2: 4})
	receiveLiveReading(t, channel)

	api.publishLive(SensorReading{DeviceID: "pi-2", Timestamp: 100, PM2: 12})
	received := receiveLiveReading(t, channel)
	if received.DeviceID != "pi-2" || received.Timestamp != 100 {
		t.Fatalf(
			"expected older reading for pi-2 to publish, got device=%q timestamp=%d",
			received.DeviceID,
			received.Timestamp,
		)
	}
}

func receiveLiveReading(t *testing.T, channel <-chan SensorReading) SensorReading {
	t.Helper()

	select {
	case received := <-channel:
		return received
	case <-time.After(time.Second):
		t.Fatal("expected published reading")
	}

	return SensorReading{}
}
