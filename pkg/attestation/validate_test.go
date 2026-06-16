package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
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

func TestValidateRemoteSVID_Valid(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	remoteTD := spiffeid.RequireTrustDomainFromString("remote.example.com")
	bundle := jwtbundle.New(remoteTD)
	_ = bundle.AddJWTAuthority("test-key-1", &key.PublicKey)

	pair := &FederationPair{
		Local:  TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		Remote: TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	svid, err := ValidateRemoteSVID(token, pair, bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svid.ID.String() != "spiffe://remote.example.com/workload/api" {
		t.Errorf("SVID ID = %q, want %q", svid.ID, "spiffe://remote.example.com/workload/api")
	}
}

func TestValidateRemoteSVID_WrongAudience(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	remoteTD := spiffeid.RequireTrustDomainFromString("remote.example.com")
	bundle := jwtbundle.New(remoteTD)
	_ = bundle.AddJWTAuthority("test-key-1", &key.PublicKey)

	pair := &FederationPair{
		Local:  TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		Remote: TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"wrong-audience.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = ValidateRemoteSVID(token, pair, bundle)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateRemoteSVID_WrongTrustDomain(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	// Bundle is for "attacker.example.com" but pair expects "remote.example.com"
	attackerTD := spiffeid.RequireTrustDomainFromString("attacker.example.com")
	bundle := jwtbundle.New(attackerTD)
	_ = bundle.AddJWTAuthority("test-key-1", &key.PublicKey)

	pair := &FederationPair{
		Local:  TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		Remote: TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://attacker.example.com/workload/evil",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = ValidateRemoteSVID(token, pair, bundle)
	if err == nil {
		t.Fatal("expected error for wrong trust domain")
	}
}

func TestValidateRemoteSVID_ExpiredToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	remoteTD := spiffeid.RequireTrustDomainFromString("remote.example.com")
	bundle := jwtbundle.New(remoteTD)
	_ = bundle.AddJWTAuthority("test-key-1", &key.PublicKey)

	pair := &FederationPair{
		Local:  TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		Remote: TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	}

	token := signTestJWT(t, key, "test-key-1", map[string]any{
		"sub": "spiffe://remote.example.com/workload/api",
		"aud": []string{"local.example.com"},
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	_, err = ValidateRemoteSVID(token, pair, bundle)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateRemoteSVID_InvalidToken(t *testing.T) {
	remoteTD := spiffeid.RequireTrustDomainFromString("remote.example.com")
	bundle := jwtbundle.New(remoteTD)

	pair := &FederationPair{
		Local:  TrustDomain{Name: "local.example.com", Cloud: "gcp"},
		Remote: TrustDomain{Name: "remote.example.com", Cloud: "aws"},
	}

	_, err := ValidateRemoteSVID("not-a-jwt", pair, bundle)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}
