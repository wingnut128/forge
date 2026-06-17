package attestation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// makeBundleJSON builds a marshaled SPIFFE bundle carrying one JWT authority
// per kid and a 5-minute refresh hint, for use as a test endpoint payload.
func makeBundleJSON(t *testing.T, td string, kids ...string) []byte {
	t.Helper()
	auth := map[string]crypto.PublicKey{}
	for _, kid := range kids {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generating key: %v", err)
		}
		auth[kid] = key.Public()
	}
	b := spiffebundle.FromJWTAuthorities(spiffeid.RequireTrustDomainFromString(td), auth)
	b.SetRefreshHint(5 * time.Minute)
	data, err := b.Marshal()
	if err != nil {
		t.Fatalf("marshaling bundle: %v", err)
	}
	return data
}

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

// serveBundleEndpoint starts a test HTTP server that serves a SPIFFE bundle
// carrying a single JWT authority.
func serveBundleEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	data := makeBundleJSON(t, "example.org", "key-1")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
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

	// A successful fetch records the refresh time so callers can detect staleness.
	if r.LastRefresh().IsZero() {
		t.Error("LastRefresh should be set after a successful fetch")
	}
	// The served bundle's 5-minute refresh hint should drive the cadence.
	if got := r.nextInterval(); got != 5*time.Minute {
		t.Errorf("nextInterval = %v, want refresh hint 5m", got)
	}
}

func TestBundleRefresher_RejectsEmptyBundle(t *testing.T) {
	// A bundle with no authorities must not replace/seed the trust root.
	empty := map[string]any{"keys": []any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(empty)
	}))
	defer srv.Close()

	r, err := NewBundleRefresher("example.org", srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := r.Start(ctx); err == nil {
		t.Fatal("expected Start to reject an empty trust bundle")
	}
}

func TestBundleRefresher_NextIntervalFloor(t *testing.T) {
	// With no bundle yet, the cadence falls back to the configured interval,
	// floored by minRefreshInterval.
	r, err := NewBundleRefresher("example.org", "https://bundle.example.org", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.nextInterval(); got != minRefreshInterval {
		t.Errorf("nextInterval = %v, want floor %v", got, minRefreshInterval)
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
