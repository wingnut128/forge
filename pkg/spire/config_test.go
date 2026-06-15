package spire

import (
	"os"
	"testing"
)

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return string(b)
}

func TestRenderServerHCL_GCPDisk(t *testing.T) {
	cfg := ServerConfig{
		TrustDomain:           "forge.gcp.local",
		PeerTrustDomain:       "forge.aws.local",
		PeerBundleEndpointURL: "https://spire-aws-server:8443",
		StateMode:             StateModeDisk,
	}
	got, err := RenderServerHCL(cfg)
	if err != nil {
		t.Fatalf("RenderServerHCL: %v", err)
	}
	want := readGolden(t, "server_gcp_disk.golden")
	if got != want {
		t.Errorf("rendered HCL mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderServerHCL_RequiresFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServerConfig
	}{
		{
			name: "missing TrustDomain",
			cfg: ServerConfig{
				PeerTrustDomain:       "forge.aws.local",
				PeerBundleEndpointURL: "https://spire-aws-server:8443",
				StateMode:             StateModeDisk,
			},
		},
		{
			name: "missing PeerTrustDomain",
			cfg: ServerConfig{
				TrustDomain:           "forge.gcp.local",
				PeerBundleEndpointURL: "https://spire-aws-server:8443",
				StateMode:             StateModeDisk,
			},
		},
		{
			name: "missing PeerBundleEndpointURL",
			cfg: ServerConfig{
				TrustDomain:     "forge.gcp.local",
				PeerTrustDomain: "forge.aws.local",
				StateMode:       StateModeDisk,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RenderServerHCL(tc.cfg)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
