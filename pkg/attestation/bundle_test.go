package attestation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

const testTD = "example.org"

// testAuthority is a peer trust domain's X.509 CA: it signs the bundle
// endpoint's serving SVID, the way a SPIRE server's CA signs its own SVID
// under the https_spiffe profile.
type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestAuthority(t *testing.T) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatalf("creating CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}
	return &testAuthority{cert: cert, key: key}
}

// svid issues a serving certificate whose only identity is a SPIFFE ID URI
// SAN — no DNS names, exactly what SPIRE's bundle endpoint presents.
func (a *testAuthority) svid(t *testing.T, id string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating SVID key: %v", err)
	}
	u, err := url.Parse(id)
	if err != nil {
		t.Fatalf("parsing SPIFFE ID: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, key.Public(), a.key)
	if err != nil {
		t.Fatalf("creating SVID: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// makeBundle builds a bundle carrying the given X.509 authorities, one JWT
// authority per kid, and a 5-minute refresh hint.
func makeBundle(t *testing.T, x509Auth []*x509.Certificate, kids ...string) *spiffebundle.Bundle {
	t.Helper()
	b := spiffebundle.New(spiffeid.RequireTrustDomainFromString(testTD))
	for _, c := range x509Auth {
		b.AddX509Authority(c)
	}
	for _, kid := range kids {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generating key: %v", err)
		}
		if err := b.AddJWTAuthority(kid, key.Public()); err != nil {
			t.Fatalf("adding JWT authority: %v", err)
		}
	}
	b.SetRefreshHint(5 * time.Minute)
	return b
}

func marshal(t *testing.T, b *spiffebundle.Bundle) []byte {
	t.Helper()
	data, err := b.Marshal()
	if err != nil {
		t.Fatalf("marshaling bundle: %v", err)
	}
	return data
}

// serveSPIFFEEndpoint starts a TLS bundle endpoint presenting the given
// serving SVID and returning body for every request.
func serveSPIFFEEndpoint(t *testing.T, serving tls.Certificate, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serving}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// fixture is a peer trust domain with a seed bundle (what the bootstrap
// exchanged) and a correctly authenticated endpoint serving a fresh bundle.
type fixture struct {
	ca   *testAuthority
	seed *spiffebundle.Bundle
	srv  *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ca := newTestAuthority(t)
	served := makeBundle(t, []*x509.Certificate{ca.cert}, "key-2")
	return &fixture{
		ca:   ca,
		seed: makeBundle(t, []*x509.Certificate{ca.cert}, "key-1"),
		srv:  serveSPIFFEEndpoint(t, ca.svid(t, "spiffe://"+testTD+"/spire/server"), marshal(t, served)),
	}
}

func startCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestNewBundleRefresher_Valid(t *testing.T) {
	seed := makeBundle(t, []*x509.Certificate{newTestAuthority(t).cert}, "key-1")
	r, err := NewBundleRefresher(seed, "https://bundle.example.org", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != DefaultRefreshInterval {
		t.Errorf("interval = %v, want %v", r.interval, DefaultRefreshInterval)
	}
	if got := r.endpointID.String(); got != "spiffe://"+testTD+"/spire/server" {
		t.Errorf("endpointID = %q, want the peer SPIRE server's ID", got)
	}
}

func TestNewBundleRefresher_CustomInterval(t *testing.T) {
	seed := makeBundle(t, []*x509.Certificate{newTestAuthority(t).cert}, "key-1")
	r, err := NewBundleRefresher(seed, "https://bundle.example.org", 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != 10*time.Minute {
		t.Errorf("interval = %v, want %v", r.interval, 10*time.Minute)
	}
}

func TestNewBundleRefresher_RequiresSeed(t *testing.T) {
	if _, err := NewBundleRefresher(nil, "https://bundle.example.org", time.Minute); err == nil {
		t.Fatal("expected error for nil seed bundle")
	}
}

func TestNewBundleRefresher_SeedNeedsX509Authorities(t *testing.T) {
	// Without X.509 authorities there is nothing to authenticate the endpoint
	// against, so every fetch would fail.
	seed := makeBundle(t, nil, "key-1")
	if _, err := NewBundleRefresher(seed, "https://bundle.example.org", time.Minute); err == nil {
		t.Fatal("expected error for seed bundle without X.509 authorities")
	}
}

func TestNewBundleRefresher_EmptyEndpointURL(t *testing.T) {
	seed := makeBundle(t, []*x509.Certificate{newTestAuthority(t).cert}, "key-1")
	if _, err := NewBundleRefresher(seed, "", time.Minute); err == nil {
		t.Fatal("expected error for empty endpoint URL")
	}
}

func TestBundleRefresher_WrongTrustDomain(t *testing.T) {
	seed := makeBundle(t, []*x509.Certificate{newTestAuthority(t).cert}, "key-1")
	r, err := NewBundleRefresher(seed, "https://bundle.example.org", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td := spiffeid.RequireTrustDomainFromString("other.org")
	if _, err := r.GetJWTBundleForTrustDomain(td); err == nil {
		t.Fatal("expected error for wrong trust domain")
	}
}

func TestBundleRefresher_FetchAndGet(t *testing.T) {
	f := newFixture(t)
	r, err := NewBundleRefresher(f.seed, f.srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	td := spiffeid.RequireTrustDomainFromString(testTD)
	bundle, err := r.GetJWTBundleForTrustDomain(td)
	if err != nil {
		t.Fatalf("GetJWTBundleForTrustDomain failed: %v", err)
	}
	// The fetched bundle, not the seed, is what validation now uses.
	if _, ok := bundle.FindJWTAuthority("key-2"); !ok {
		t.Error("expected the fetched bundle's JWT authority after Start")
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

func TestBundleRefresher_RejectsUntrustedEndpoint(t *testing.T) {
	// An endpoint whose SVID chains to a CA outside the seed bundle must be
	// refused — this is the authentication the web-PKI client never did.
	f := newFixture(t)
	impostor := newTestAuthority(t)
	srv := serveSPIFFEEndpoint(t, impostor.svid(t, "spiffe://"+testTD+"/spire/server"),
		marshal(t, makeBundle(t, []*x509.Certificate{impostor.cert}, "evil")))

	r, err := NewBundleRefresher(f.seed, srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to reject an endpoint not signed by the seed CA")
	}
}

func TestBundleRefresher_RejectsWrongEndpointID(t *testing.T) {
	// A valid SVID from the right trust domain is still not the SPIRE server.
	f := newFixture(t)
	srv := serveSPIFFEEndpoint(t, f.ca.svid(t, "spiffe://"+testTD+"/workload/other"),
		marshal(t, makeBundle(t, []*x509.Certificate{f.ca.cert}, "key-2")))

	r, err := NewBundleRefresher(f.seed, srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to reject an endpoint presenting the wrong SPIFFE ID")
	}
}

func TestBundleRefresher_RejectsEmptyBundle(t *testing.T) {
	// A bundle with no authorities must not replace the trust root.
	f := newFixture(t)
	body, _ := json.Marshal(map[string]any{"keys": []any{}})
	srv := serveSPIFFEEndpoint(t, f.ca.svid(t, "spiffe://"+testTD+"/spire/server"), body)

	r, err := NewBundleRefresher(f.seed, srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to reject an empty trust bundle")
	}
}

func TestBundleRefresher_RejectsBundleWithoutX509Authorities(t *testing.T) {
	// Accepting it would leave the next fetch nothing to authenticate against,
	// permanently breaking refresh.
	f := newFixture(t)
	srv := serveSPIFFEEndpoint(t, f.ca.svid(t, "spiffe://"+testTD+"/spire/server"),
		marshal(t, makeBundle(t, nil, "key-2")))

	r, err := NewBundleRefresher(f.seed, srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to reject a bundle with no X.509 authorities")
	}
}

func TestBundleRefresher_NextIntervalFloor(t *testing.T) {
	// A seed without a refresh hint falls back to the configured interval,
	// floored by minRefreshInterval.
	seed := spiffebundle.New(spiffeid.RequireTrustDomainFromString(testTD))
	seed.AddX509Authority(newTestAuthority(t).cert)
	r, err := NewBundleRefresher(seed, "https://bundle.example.org", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.nextInterval(); got != minRefreshInterval {
		t.Errorf("nextInterval = %v, want floor %v", got, minRefreshInterval)
	}
}

func TestBundleRefresher_StartFailsOnBadEndpoint(t *testing.T) {
	f := newFixture(t)
	r, err := NewBundleRefresher(f.seed, "https://127.0.0.1:1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to fail with unreachable endpoint")
	}
}

func TestBundleRefresher_StartFailsOnNonOKStatus(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{f.ca.svid(t, "spiffe://"+testTD+"/spire/server")},
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	defer srv.Close()

	r, err := NewBundleRefresher(f.seed, srv.URL, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Start(startCtx(t)); err == nil {
		t.Fatal("expected Start to fail on HTTP 500")
	}
}

func TestLoadSeedBundle(t *testing.T) {
	seed := makeBundle(t, []*x509.Certificate{newTestAuthority(t).cert}, "key-1")
	path := filepath.Join(t.TempDir(), "peer.bundle")
	if err := os.WriteFile(path, marshal(t, seed), 0o600); err != nil {
		t.Fatalf("writing seed: %v", err)
	}

	got, err := LoadSeedBundle(testTD, path)
	if err != nil {
		t.Fatalf("LoadSeedBundle failed: %v", err)
	}
	if !got.Equal(seed) {
		t.Error("loaded bundle does not match the one written")
	}
}

func TestLoadSeedBundle_Errors(t *testing.T) {
	if _, err := LoadSeedBundle("", "/nonexistent"); err == nil {
		t.Error("expected error for empty trust domain")
	}
	if _, err := LoadSeedBundle(testTD, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for missing seed file")
	}
}
