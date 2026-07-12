package server

import "testing"

const validReadingPayload = `{
  "timestamp":"1738886400",
  "temperature":"22.4",
  "pressure":"101305",
  "humidity":"40.1",
  "oxidised":"1.2",
  "reduced":"1.1",
  "nh3":"0.7",
  "pm1":"2",
  "pm2":"3",
  "pm10":"4"
}`

func TestDecodeReadingDefaultsLegacyParticulateDataToAvailable(t *testing.T) {
	reading, err := DecodeReading([]byte(validReadingPayload))
	if err != nil {
		t.Fatalf("decode reading: %v", err)
	}
	if !particulateAvailable(reading) {
		t.Fatal("expected legacy payload without pm_available to remain available")
	}
}

func TestDecodeReadingPreservesUnavailableParticulateStatus(t *testing.T) {
	payload := []byte(`{
      "timestamp":"1738886400",
      "temperature":"22.4",
      "pressure":"101305",
      "humidity":"40.1",
      "oxidised":"1.2",
      "reduced":"1.1",
      "nh3":"0.7",
      "pm1":"12",
      "pm2":"12",
      "pm10":"12",
      "pm_available":false
    }`)

	reading, err := DecodeReading(payload)
	if err != nil {
		t.Fatalf("decode reading: %v", err)
	}
	if particulateAvailable(reading) {
		t.Fatal("expected cached particulate values to be unavailable")
	}
}
