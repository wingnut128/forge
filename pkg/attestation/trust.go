// Package attestation implements cross-cloud workload attestation
// using SPIFFE trust bundle exchange and SVID validation.
package attestation

import "fmt"

// TrustDomain represents a SPIFFE trust domain with its federation configuration.
type TrustDomain struct {
	// Name is the SPIFFE trust domain name (e.g., "forge.dev.aws.example.com")
	Name string

	// Cloud identifies the cloud provider ("gcp", "aws")
	Cloud string
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
		return nil, fmt.Errorf("forge requires cross-cloud federation: both domains are in %q", local.Cloud)
	}

	return &FederationPair{
		Local:  local,
		Remote: remote,
	}, nil
}
