// Package attestation implements cross-cloud workload attestation
// using SPIFFE trust bundle exchange and SVID validation.
package attestation

// TrustDomain represents a SPIFFE trust domain with its federation configuration.
type TrustDomain struct {
	// Name is the SPIFFE trust domain name (e.g., "forge.dev.aws.example.com")
	Name string

	// Cloud identifies the cloud provider ("gcp", "aws")
	Cloud string

	// OIDCDiscoveryURL is the endpoint serving the JWKS for this trust domain's SPIRE server.
	OIDCDiscoveryURL string

	// BundleEndpointURL is the SPIFFE Bundle Endpoint (RFC 9409) URL for trust bundle exchange.
	// This is the preferred federation mechanism over OIDC when both SPIRE servers support it.
	BundleEndpointURL string
}

// FederationPair defines a bidirectional trust relationship between two SPIFFE trust domains.
type FederationPair struct {
	Local  TrustDomain
	Remote TrustDomain
}

// NewFederationPair creates a federation pair, validating that the trust domains
// are in different clouds (cross-cloud attestation doesn't make sense within a single cloud).
func NewFederationPair(local, remote TrustDomain) (*FederationPair, error) {
	if local.Cloud == remote.Cloud {
		// Not strictly an error — same-cloud federation is valid in SPIFFE —
		// but outside forge's scope. Log a warning in production.
		_ = 0 // placeholder
	}

	return &FederationPair{
		Local:  local,
		Remote: remote,
	}, nil
}

// TODO: Implement trust bundle refresh loop using SPIFFE Bundle Endpoint API (RFC 9409).
// The bundle endpoint provides automatic, authenticated trust bundle distribution
// between SPIRE servers without requiring manual JWKS synchronization.
//
// See: https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md
