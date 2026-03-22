package attestation

import (
	"testing"
)

func TestNewFederationPair_CrossCloud(t *testing.T) {
	gcp := TrustDomain{
		Name:             "forge.dev.gcp.example.com",
		Cloud:            "gcp",
		OIDCDiscoveryURL: "https://oidc-discovery.forge.dev.gcp.example.com",
	}
	aws := TrustDomain{
		Name:             "forge.dev.aws.example.com",
		Cloud:            "aws",
		OIDCDiscoveryURL: "https://oidc-discovery.forge.dev.aws.example.com",
	}

	pair, err := NewFederationPair(gcp, aws)
	if err != nil {
		t.Fatalf("unexpected error for cross-cloud pair: %v", err)
	}
	if pair.Local.Cloud != "gcp" {
		t.Errorf("local cloud = %q, want %q", pair.Local.Cloud, "gcp")
	}
	if pair.Remote.Cloud != "aws" {
		t.Errorf("remote cloud = %q, want %q", pair.Remote.Cloud, "aws")
	}
}

func TestNewFederationPair_SameCloudReturnsError(t *testing.T) {
	a := TrustDomain{Name: "domain-a.example.com", Cloud: "gcp"}
	b := TrustDomain{Name: "domain-b.example.com", Cloud: "gcp"}

	pair, err := NewFederationPair(a, b)
	if err == nil {
		t.Fatal("expected error for same-cloud federation, got nil")
	}
	if pair != nil {
		t.Error("expected nil pair on error")
	}
}

func TestNewFederationPair_PreservesFields(t *testing.T) {
	local := TrustDomain{
		Name:              "forge.dev.gcp.example.com",
		Cloud:             "gcp",
		OIDCDiscoveryURL:  "https://oidc.gcp.example.com",
		BundleEndpointURL: "https://bundle.gcp.example.com",
	}
	remote := TrustDomain{
		Name:              "forge.dev.aws.example.com",
		Cloud:             "aws",
		OIDCDiscoveryURL:  "https://oidc.aws.example.com",
		BundleEndpointURL: "https://bundle.aws.example.com",
	}

	pair, err := NewFederationPair(local, remote)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.Local.BundleEndpointURL != local.BundleEndpointURL {
		t.Errorf("local BundleEndpointURL = %q, want %q", pair.Local.BundleEndpointURL, local.BundleEndpointURL)
	}
	if pair.Remote.OIDCDiscoveryURL != remote.OIDCDiscoveryURL {
		t.Errorf("remote OIDCDiscoveryURL = %q, want %q", pair.Remote.OIDCDiscoveryURL, remote.OIDCDiscoveryURL)
	}
}
