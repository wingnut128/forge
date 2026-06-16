package orchestration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/wingnut128/forge/pkg/attestation"
	"github.com/wingnut128/forge/pkg/authz"
)

// signTestJWT creates a signed JWT with the given claims using ES256.
func signTestJWT(t *testing.T, key *ecdsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), keyID),
	)
	if err != nil {
		t.Fatalf("creating signer: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling claims: %v", err)
	}
	jws, err := sig.Sign(payload)
	if err != nil {
		t.Fatalf("signing JWT: %v", err)
	}
	token, err := jws.CompactSerialize()
	if err != nil {
		t.Fatalf("serializing JWT: %v", err)
	}
	return token
}

func testPairAndBundle(t *testing.T) (*attestation.FederationPair, *jwtbundle.Bundle, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	remoteTD := spiffeid.RequireTrustDomainFromString("remote.example.com")
	bundle := jwtbundle.New(remoteTD)
	_ = bundle.AddJWTAuthority("test-key-1", &key.PublicKey)

	pair, err := attestation.NewFederationPair(
		attestation.TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		attestation.TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	)
	if err != nil {
		t.Fatalf("creating pair: %v", err)
	}
	return pair, bundle, key
}

func TestHandleValidate_Success(t *testing.T) {
	pair, bundle, key := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	body, _ := json.Marshal(validateRequest{Token: token})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Valid {
		t.Errorf("valid = false, want true")
	}
	if resp.SpiffeID != "spiffe://remote.example.com/workload/api" {
		t.Errorf("spiffe_id = %q", resp.SpiffeID)
	}
	if resp.TrustDomain != "remote.example.com" {
		t.Errorf("trust_domain = %q", resp.TrustDomain)
	}
}

func TestHandleValidate_InvalidToken(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, _ := json.Marshal(validateRequest{Token: "garbage"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Error("valid = true, want false")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleValidate_MissingBody(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/validate", nil)
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleValidate_EmptyToken(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, _ := json.Marshal(validateRequest{Token: ""})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleValidate_WrongMethod(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/validate", nil)
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleHealthz_BundleLoaded(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp healthResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}

type errorBundleSource struct{}

func (e *errorBundleSource) GetJWTBundleForTrustDomain(_ spiffeid.TrustDomain) (*jwtbundle.Bundle, error) {
	return nil, fmt.Errorf("no bundle available")
}

func TestHandleHealthz_NoBundleLoaded(t *testing.T) {
	pair, _, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, &errorBundleSource{}, ":0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.handleHealthz(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	pair, bundle, _ := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for server to be listening
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + srv.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
}

// --- Authorization tests ---

// stubAuthorizer implements authz.Authorizer with a fixed decision.
type stubAuthorizer struct {
	decision authz.Decision
}

func (s *stubAuthorizer) IsAuthorized(_, _, _ string) (authz.Decision, error) {
	return s.decision, nil
}

func TestHandleValidate_WithAuthz_Permitted(t *testing.T) {
	pair, bundle, key := testPairAndBundle(t)
	az := &stubAuthorizer{decision: authz.Decision{Allowed: true, Reason: "test permit"}}
	srv, err := NewServer(pair, bundle, ":0", az)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	body, _ := json.Marshal(validateRequest{Token: token, Action: "read-data", Resource: "pipeline-x"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Valid {
		t.Error("valid = false, want true")
	}
	if resp.Authorized == nil || !*resp.Authorized {
		t.Error("authorized should be true")
	}
}

func TestHandleValidate_WithAuthz_Denied(t *testing.T) {
	pair, bundle, key := testPairAndBundle(t)
	az := &stubAuthorizer{decision: authz.Decision{Allowed: false, Reason: "no matching permit policy"}}
	srv, err := NewServer(pair, bundle, ":0", az)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	body, _ := json.Marshal(validateRequest{Token: token, Action: "write-data", Resource: "pipeline-x"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Authorized == nil || *resp.Authorized {
		t.Error("authorized should be false")
	}
	if resp.DenyReason == "" {
		t.Error("expected deny_reason")
	}
}

func TestHandleValidate_AuthzSkippedWhenNoActionResource(t *testing.T) {
	pair, bundle, key := testPairAndBundle(t)
	az := &stubAuthorizer{decision: authz.Decision{Allowed: false, Reason: "should not be called"}}
	srv, err := NewServer(pair, bundle, ":0", az)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// No action/resource — authz should be skipped
	body, _ := json.Marshal(validateRequest{Token: token})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Authorized != nil {
		t.Error("authorized should be nil when action/resource not provided")
	}
}

func TestHandleValidate_AuthzSkippedWhenNoAuthorizer(t *testing.T) {
	pair, bundle, key := testPairAndBundle(t)
	srv, err := NewServer(pair, bundle, ":0", nil) // nil authorizer
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	body, _ := json.Marshal(validateRequest{Token: token, Action: "read-data", Resource: "pipeline-x"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)

	var resp validateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Authorized != nil {
		t.Error("authorized should be nil when no authorizer configured")
	}
}
