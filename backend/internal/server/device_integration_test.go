package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Supply a test Postgres URL whose user can create temporary databases to run locally.
func TestDeviceDurableIngestPostgres(t *testing.T) {
	adminURL := os.Getenv("ENVIRO_TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("set ENVIRO_TEST_POSTGRES_URL to run the device integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	testURL, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	dbName := fmt.Sprintf("enviro_test_%d", time.Now().UnixNano())
	testURL.Path = "/" + dbName
	// Start with a real database that refuses connections, then recover it in place.
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName+" ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+dbName); err != nil {
			t.Errorf("clean up temporary database: %v", err)
		}
	}()
	store := NewRuntimeStore(nil)
	defer store.Close()
	store.SetConnector(func(ctx context.Context) (Store, error) {
		return NewPostgresStore(ctx, testURL.String(), 2)
	})
	handler := NewAPI(store, "integration-key").Handler()
	var loseNextReply atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if loseNextReply.CompareAndSwap(true, false) {
			handler.ServeHTTP(httptest.NewRecorder(), r)
			panic(http.ErrAbortHandler) // The backend commits, but the device receives no response.
		}
		handler.ServeHTTP(w, r)
	}))
	defer api.Close()

	queue := filepath.Join(t.TempDir(), "pending.json")
	runDevice := func(sample string, expectAck bool) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "python3", "-c", deviceIngestDriver,
			api.URL, queue, sample, strconv.FormatBool(expectAck))
		cmd.Dir = "../../.." // repository root for the device module
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("device %s, expected acknowledgement %t: %v\n%s", sample, expectAck, err, out)
		}
	}
	runDevice("valid-pm", false)
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+dbName+" ALLOW_CONNECTIONS true"); err != nil {
		t.Fatal(err)
	}
	loseNextReply.Store(true)
	runDevice("replay", false)
	db, err := pgx.Connect(ctx, testURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	assertOneRow := func() {
		t.Helper()
		var count int
		if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM sensor_readings").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected one stored sample, got %d", count)
		}
	}
	assertOneRow()
	runDevice("replay", true)
	assertOneRow()

	runDevice("missing-pm", true)
	response, err := api.Client().Get(api.URL + "/api/readings?device_id=test-pi&from=100&to=160&max_points=1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var history struct{ Readings []SensorReading }
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(history.Readings) != 1 {
		t.Fatalf("expected one history bucket, got HTTP %d and %+v", response.StatusCode, history)
	}
	bucket := history.Readings[0]
	if bucket.PMAvailable == nil || !*bucket.PMAvailable || bucket.LatestPMAvailable == nil ||
		*bucket.LatestPMAvailable || bucket.PM2 != 3 || bucket.Timestamp != 160 {
		t.Fatalf("incorrect mixed availability bucket: %+v", bucket)
	}
}

// Each invocation starts a fresh transmitter with the same on-disk queue.
const deviceIngestDriver = `
import json
import sys
from pathlib import Path
from device.backend_transmitter import BackendTransmitter

url, queue_file, sample, expected = sys.argv[1:]
device = BackendTransmitter(url, "integration-key", queue_file=queue_file,
                            device_id="test-pi", live_interval_seconds=0)
if sample != "replay":
    valid = sample == "valid-pm"
    device.send({
        "timestamp": "100" if valid else "160", "temperature": "22", "pressure": "101000",
        "humidity": "50", "oxidised": "1", "reduced": "1", "nh3": "1",
        "pm1": "2", "pm2": "3" if valid else "99", "pm10": "4", "pm_available": valid,
    })
else:
    assert len(device.pending) == 1, device.pending
acknowledged = device.flush()
assert acknowledged == (expected == "true"), acknowledged
pending = json.loads(Path(queue_file).read_text())
assert len(pending) == (0 if acknowledged else 1), pending
`
