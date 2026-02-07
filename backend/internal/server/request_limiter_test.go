package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIdentityIgnoresForwardedHeadersByDefault(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
	request.RemoteAddr = "203.0.113.7:5050"
	request.Header.Set("X-Forwarded-For", "198.51.100.1")
	request.Header.Set("X-Real-IP", "198.51.100.2")

	identity := clientIdentity(request, false)
	if identity != "203.0.113.7" {
		t.Fatalf("expected remote ip identity, got %q", identity)
	}
}

func TestClientIdentityUsesForwardedHeadersWhenTrusted(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
	request.RemoteAddr = "203.0.113.7:5050"
	request.Header.Set("X-Forwarded-For", "198.51.100.1, 198.51.100.8")

	identity := clientIdentity(request, true)
	if identity != "198.51.100.1" {
		t.Fatalf("expected forwarded identity, got %q", identity)
	}
}
