package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type SensorReading struct {
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

var allowedReadingKeys = map[string]struct{}{
	"timestamp":   {},
	"temperature": {},
	"pressure":    {},
	"humidity":    {},
	"oxidised":    {},
	"reduced":     {},
	"nh3":         {},
	"pm1":         {},
	"pm2":         {},
	"pm10":        {},
}

func DecodeReading(raw []byte) (SensorReading, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return SensorReading{}, err
	}

	return decodeReadingPayload(payload)
}

func DecodeReadingsBatch(raw []byte, maxBatchSize int) ([]SensorReading, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var payloads []map[string]any
	if err := decoder.Decode(&payloads); err != nil {
		return nil, err
	}

	if len(payloads) == 0 {
		return nil, fmt.Errorf("batch must include at least one reading")
	}
	if len(payloads) > maxBatchSize {
		return nil, fmt.Errorf("batch exceeds max size of %d", maxBatchSize)
	}

	readings := make([]SensorReading, 0, len(payloads))
	for index, payload := range payloads {
		reading, err := decodeReadingPayload(payload)
		if err != nil {
			return nil, fmt.Errorf("invalid reading at index %d: %w", index, err)
		}
		readings = append(readings, reading)
	}

	return readings, nil
}

func decodeReadingPayload(payload map[string]any) (SensorReading, error) {
	for key := range payload {
		if _, allowed := allowedReadingKeys[key]; !allowed {
			return SensorReading{}, fmt.Errorf("unknown field: %s", key)
		}
	}

	timestamp, err := parseInt64Field(payload, "timestamp")
	if err != nil {
		return SensorReading{}, err
	}
	if timestamp == 0 {
		return SensorReading{}, fmt.Errorf("timestamp is required")
	}

	temperature, err := parseFloatField(payload, "temperature")
	if err != nil {
		return SensorReading{}, err
	}
	pressure, err := parseFloatField(payload, "pressure")
	if err != nil {
		return SensorReading{}, err
	}
	humidity, err := parseFloatField(payload, "humidity")
	if err != nil {
		return SensorReading{}, err
	}
	oxidised, err := parseFloatField(payload, "oxidised")
	if err != nil {
		return SensorReading{}, err
	}
	reduced, err := parseFloatField(payload, "reduced")
	if err != nil {
		return SensorReading{}, err
	}
	nh3, err := parseFloatField(payload, "nh3")
	if err != nil {
		return SensorReading{}, err
	}
	pm1, err := parseFloatField(payload, "pm1")
	if err != nil {
		return SensorReading{}, err
	}
	pm2, err := parseFloatField(payload, "pm2")
	if err != nil {
		return SensorReading{}, err
	}
	pm10, err := parseFloatField(payload, "pm10")
	if err != nil {
		return SensorReading{}, err
	}

	return SensorReading{
		Timestamp:   timestamp,
		Temperature: temperature,
		Pressure:    pressure,
		Humidity:    humidity,
		Oxidised:    oxidised,
		Reduced:     reduced,
		Nh3:         nh3,
		PM1:         pm1,
		PM2:         pm2,
		PM10:        pm10,
	}, nil
}

func parseFloatField(payload map[string]any, key string) (float64, error) {
	value, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("missing field: %s", key)
	}

	parsed, err := parseFloat(value)
	if err != nil {
		return 0, fmt.Errorf("invalid field %s: %w", key, err)
	}
	return parsed, nil
}

func parseInt64Field(payload map[string]any, key string) (int64, error) {
	value, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("missing field: %s", key)
	}

	parsed, err := parseInt64(value)
	if err != nil {
		return 0, fmt.Errorf("invalid field %s: %w", key, err)
	}
	return parsed, nil
}

func parseFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Float64()
	case string:
		return strconv.ParseFloat(typed, 64)
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	default:
		return 0, fmt.Errorf("unsupported number type %T", value)
	}
}

func parseInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case string:
		if intValue, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return intValue, nil
		}
		floatValue, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, err
		}
		return int64(floatValue), nil
	case float64:
		return int64(typed), nil
	case float32:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	default:
		return 0, fmt.Errorf("unsupported integer type %T", value)
	}
}
