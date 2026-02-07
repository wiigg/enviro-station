package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"time"
)

type sensorReading struct {
	Timestamp   int64   `json:"timestamp"`
	Temperature float64 `json:"temperature"`
	Pressure    float64 `json:"pressure"`
	Humidity    float64 `json:"humidity"`
	Oxidised    float64 `json:"oxidised"`
	Reduced     float64 `json:"reduced"`
	Nh3         float64 `json:"nh3"`
	PM1         float64 `json:"pm1"`
	PM2         float64 `json:"pm2"`
	PM10        float64 `json:"pm10"`
}

type simulator struct {
	temperature float64
	pressure    float64
	humidity    float64
	oxidised    float64
	reduced     float64
	nh3         float64
	pm2Baseline float64
}

func main() {
	var targetURL string
	var apiKey string
	var interval time.Duration
	var jitter time.Duration
	var timeout time.Duration
	var count int
	var seed int64

	flag.StringVar(&targetURL, "url", "http://localhost:8080/api/ingest", "ingest endpoint URL")
	flag.StringVar(&apiKey, "api-key", "dev-ingest-key", "ingest API key")
	flag.DurationVar(&interval, "interval", 2*time.Second, "base delay between emitted readings")
	flag.DurationVar(&jitter, "jitter", 500*time.Millisecond, "max random delay added to each interval")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "HTTP request timeout")
	flag.IntVar(&count, "count", 0, "number of readings to emit (0 = infinite)")
	flag.Int64Var(&seed, "seed", 0, "random seed (0 = use current time)")
	flag.Parse()

	if interval <= 0 {
		log.Fatal("interval must be > 0")
	}
	if jitter < 0 {
		log.Fatal("jitter must be >= 0")
	}
	if timeout <= 0 {
		log.Fatal("timeout must be > 0")
	}
	if count < 0 {
		log.Fatal("count must be >= 0")
	}
	if apiKey == "" {
		log.Fatal("api-key is required")
	}

	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	log.Printf("simulator started seed=%d target=%s interval=%s", seed, targetURL, interval)

	client := &http.Client{Timeout: timeout}
	model := simulator{
		temperature: 21.0,
		pressure:    1013.2,
		humidity:    46.0,
		oxidised:    1.1,
		reduced:     0.9,
		nh3:         0.7,
		pm2Baseline: 5.0,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	emitted := 0
	for {
		if count > 0 && emitted >= count {
			log.Printf("simulation complete (%d readings sent)", emitted)
			return
		}

		reading := model.next(rng, time.Now())
		if err := postReading(ctx, client, targetURL, apiKey, reading); err != nil {
			log.Printf("send failed: %v", err)
		} else {
			emitted++
			log.Printf(
				"sent #%d pm2=%.1f pm10=%.1f temp=%.1f humidity=%.1f",
				emitted,
				reading.PM2,
				reading.PM10,
				reading.Temperature,
				reading.Humidity,
			)
		}

		delay := interval
		if jitter > 0 {
			delay += time.Duration(rng.Int63n(int64(jitter) + 1))
		}

		select {
		case <-ctx.Done():
			log.Printf("simulation stopped")
			return
		case <-time.After(delay):
		}
	}
}

func (sim *simulator) next(rng *rand.Rand, now time.Time) sensorReading {
	sim.temperature = clamp(sim.temperature+rng.NormFloat64()*0.15, 16.0, 32.0)
	sim.pressure = clamp(sim.pressure+rng.NormFloat64()*0.25, 995.0, 1030.0)
	sim.humidity = clamp(sim.humidity+rng.NormFloat64()*0.7, 25.0, 80.0)
	sim.oxidised = clamp(sim.oxidised+rng.NormFloat64()*0.03, 0.2, 2.5)
	sim.reduced = clamp(sim.reduced+rng.NormFloat64()*0.03, 0.2, 2.5)
	sim.nh3 = clamp(sim.nh3+rng.NormFloat64()*0.03, 0.1, 2.0)
	sim.pm2Baseline = clamp(sim.pm2Baseline+rng.NormFloat64()*0.4, 1.0, 20.0)

	pm2 := clamp(sim.pm2Baseline+rng.NormFloat64()*0.8, 0.4, 150.0)

	// Occasional spikes mimic short-lived pollution events.
	if rng.Float64() < 0.04 {
		pm2 = clamp(pm2+rng.Float64()*40.0+8.0, 0.4, 150.0)
	}

	pm1 := clamp(pm2*0.62+rng.NormFloat64()*0.45, 0.2, 140.0)
	pm10 := clamp(pm2*1.45+rng.NormFloat64()*0.8, 0.5, 180.0)

	return sensorReading{
		Timestamp:   now.UnixMilli(),
		Temperature: round1(sim.temperature),
		Pressure:    round1(sim.pressure),
		Humidity:    round1(sim.humidity),
		Oxidised:    round2(sim.oxidised),
		Reduced:     round2(sim.reduced),
		Nh3:         round2(sim.nh3),
		PM1:         round1(pm1),
		PM2:         round1(pm2),
		PM10:        round1(pm10),
	}
}

func postReading(
	ctx context.Context,
	client *http.Client,
	targetURL string,
	apiKey string,
	reading sensorReading,
) error {
	body, err := json.Marshal(reading)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", apiKey)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("status %d: %s", response.StatusCode, string(responseBody))
	}

	return nil
}

func clamp(value float64, min float64, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
