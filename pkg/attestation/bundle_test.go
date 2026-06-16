package attestation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

func TestNewBundleRefresher_Valid(t *testing.T) {
	r, err := NewBundleRefresher("example.org", "https://bundle.example.org", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != DefaultRefreshInterval {
		t.Errorf("interval = %v, want %v", r.interval, DefaultRefreshInterval)
	}
}

func TestNewBundleRefresher_CustomInterval(t *testing.T) {
	r, err := NewBundleRefresher("example.org", "https://bundle.example.org", 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != 10*time.Minute {
		t.Errorf("interval = %v, want %v", r.interval, 10*time.Minute)
	}
}

func TestNewBundleRefresher_InvalidTrustDomain(t *testing.T) {
	_, err := NewBundleRefresher("", "https://bundle.example.org", time.Minute)
	if err == nil {
		t.Fatal("expected error for empty trust domain")
	}
}

func TestNewBundleRefresher_EmptyEndpointURL(t *testing.T) {
	_, err := NewBundleRefresher("example.org", "", time.Minute)
	if err == nil {
		t.Fatal("expected error for empty endpoint URL")
	}
}

func TestBundleRefresher_NoBundleBeforeFetch(t *testing.T) {
	r, err := NewBundleRefresher("example.org", "https://bundle.example.org", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td := spiffeid.RequireTrustDomainFromString("example.org")
	_, err = r.GetJWTBundleForTrustDomain(td)
	if err == nil {
		t.Fatal("expected error when no bundle fetched")
	}
}

func TestBundleRefresher_WrongTrustDomain(t *testing.T) {
	r, err := NewBundleRefresher("example.org", "https://bundle.example.org", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td := spiffeid.RequireTrustDomainFromString("other.org")
	_, err = r.GetJWTBundleForTrustDomain(td)
	if err == nil {
		t.Fatal("expected error for wrong trust domain")
	}
}

// serveBundleEndpoint starts a test HTTP server that serves a minimal SPIFFE bundle (JWKS).
func serveBundleEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	// Minimal valid SPIFFE bundle: empty JWKS with spiffe_refresh_hint.
	bundle := map[string]any{
		"keys":                []any{},
		"spiffe_refresh_hint": 300,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(bundle)
	}))
}

func TestBundleRefresher_FetchAndGet(t *testing.T) {
	srv := serveBundleEndpoint(t)
	defer srv.Close()

	r, err := NewBundleRefresher("example.org", srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle, err := r.GetJWTBundleForTrustDomain(td)
	if err != nil {
		t.Fatalf("GetJWTBundleForTrustDomain failed: %v", err)
	}
	if bundle == nil {
		t.Fatal("expected non-nil bundle")
	}
}

func TestBundleRefresher_StartFailsOnBadEndpoint(t *testing.T) {
	r, err := NewBundleRefresher("example.org", "http://127.0.0.1:1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := r.Start(ctx); err == nil {
		t.Fatal("expected Start to fail with unreachable endpoint")
	}
}

func TestBundleRefresher_StartFailsOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r, err := NewBundleRefresher("example.org", srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := r.Start(ctx); err == nil {
		t.Fatal("expected Start to fail on HTTP 500")
	}
}
