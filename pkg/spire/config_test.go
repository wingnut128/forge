package spire

import (
	"os"
	"strings"
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

func TestRenderServerHCL_AWSManaged(t *testing.T) {
	cfg := ServerConfig{
		TrustDomain:           "forge.aws.local",
		PeerTrustDomain:       "forge.gcp.local",
		PeerBundleEndpointURL: "https://spire-gcp-server:8443",
		StateMode:             StateModeManaged,
		ManagedDBConnString:   "postgres://spire@db.example:5432/spire",
	}
	got, err := RenderServerHCL(cfg)
	if err != nil {
		t.Fatalf("RenderServerHCL: %v", err)
	}
	want := readGolden(t, "server_aws_managed.golden")
	if got != want {
		t.Errorf("rendered HCL mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderServerHCL_ManagedRequiresConnString(t *testing.T) {
	_, err := RenderServerHCL(ServerConfig{
		TrustDomain:           "forge.aws.local",
		PeerTrustDomain:       "forge.gcp.local",
		PeerBundleEndpointURL: "https://spire-gcp-server:8443",
		StateMode:             StateModeManaged,
	})
	if err == nil {
		t.Fatal("expected error for managed mode without conn string")
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

func TestRenderAgentHCL_GCP(t *testing.T) {
	cfg := AgentConfig{
		TrustDomain:   "forge.gcp.local",
		ServerAddress: "spire-gcp-server",
	}
	got, err := RenderAgentHCL(cfg)
	if err != nil {
		t.Fatalf("RenderAgentHCL: %v", err)
	}
	want := readGolden(t, "agent_gcp.golden")
	if got != want {
		t.Errorf("rendered HCL mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderAgentHCL_RequiresFields(t *testing.T) {
	if _, err := RenderAgentHCL(AgentConfig{}); err == nil {
		t.Fatal("expected error for missing trust domain")
	}
}

func TestRenderServerStartupScript_VerifiesDownload(t *testing.T) {
	script := RenderServerStartupScript("1.11.2", "server {}", "/dev/xvdf")

	// The SPIRE binary pull must be checksum-verified and fail closed.
	for _, want := range []string{
		"_sha256sum.txt",    // fetches the published checksum companion
		"sha256sum -c",      // verifies the archive against it before use
		"--proto '=https'",  // refuses plaintext redirects
		"set -euo pipefail", // a failed verification aborts the script
	} {
		if !strings.Contains(script, want) {
			t.Errorf("startup script missing %q:\n%s", want, script)
		}
	}

	// The unverified one-shot download must be gone.
	if strings.Contains(script, "curl -sSL -o spire.tar.gz") {
		t.Error("startup script still uses the unverified download")
	}
}
