package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type FlexibleFloat float64

type FlexibleInt64 int64

func (value *FlexibleFloat) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		*value = 0
		return nil
	}

	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}

	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("invalid float value %q", text)
	}
	*value = FlexibleFloat(parsed)
	return nil
}

func (value FlexibleFloat) Float64() float64 {
	return float64(value)
}

func (value *FlexibleInt64) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		*value = 0
		return nil
	}

	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}

	if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
		*value = FlexibleInt64(parsed)
		return nil
	}

	parsedFloat, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("invalid int value %q", text)
	}

	*value = FlexibleInt64(parsedFloat)
	return nil
}

func (value FlexibleInt64) Int64() int64 {
	return int64(value)
}

type SensorReading struct {
	Timestamp   FlexibleInt64 `json:"timestamp"`
	Temperature FlexibleFloat `json:"temperature"`
	Pressure    FlexibleFloat `json:"pressure"`
	Humidity    FlexibleFloat `json:"humidity"`
	Oxidised    FlexibleFloat `json:"oxidised"`
	Reduced     FlexibleFloat `json:"reduced"`
	Nh3         FlexibleFloat `json:"nh3"`
	PM1         FlexibleFloat `json:"pm1"`
	PM2         FlexibleFloat `json:"pm2"`
	PM10        FlexibleFloat `json:"pm10"`
}

func DecodeReading(raw []byte) (SensorReading, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var reading SensorReading
	if err := decoder.Decode(&reading); err != nil {
		return SensorReading{}, err
	}

	if reading.Timestamp.Int64() == 0 {
		return SensorReading{}, fmt.Errorf("timestamp is required")
	}

	return reading, nil
}
