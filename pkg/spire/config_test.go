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

func TestRenderServerHCL_DefaultsToSPIFFEProfile(t *testing.T) {
	got, err := RenderServerHCL(ServerConfig{
		TrustDomain:           "forge.gcp.local",
		PeerTrustDomain:       "forge.aws.local",
		PeerBundleEndpointURL: "https://10.1.0.10:8443",
		StateMode:             StateModeDisk,
	})
	if err != nil {
		t.Fatalf("RenderServerHCL: %v", err)
	}
	// https_spiffe authenticates with the server's own SVID, so no serving
	// certificate should be emitted at all.
	if strings.Contains(got, "serving_cert_file") || strings.Contains(got, "https_web") {
		t.Errorf("default profile should not reference web PKI:\n%s", got)
	}
	if !strings.Contains(got, `endpoint_spiffe_id = "spiffe://forge.aws.local/spire/server"`) {
		t.Errorf("https_spiffe requires endpoint_spiffe_id:\n%s", got)
	}
}

func TestRenderServerHCL_WebProfileEmitsServingCert(t *testing.T) {
	got, err := RenderServerHCL(ServerConfig{
		TrustDomain:           "forge.gcp.local",
		PeerTrustDomain:       "forge.aws.local",
		PeerBundleEndpointURL: "https://spire-aws-server:8443",
		StateMode:             StateModeDisk,
		BundleProfile:         BundleProfileWeb,
	})
	if err != nil {
		t.Fatalf("RenderServerHCL: %v", err)
	}
	for _, want := range []string{
		`profile "https_web"`,
		"serving_cert_file",
		"/etc/spire/certs/server.crt",
		"file_sync_interval",
		`bundle_endpoint_profile "https_web" {}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("web profile missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "endpoint_spiffe_id") {
		t.Errorf("https_web must not emit endpoint_spiffe_id:\n%s", got)
	}
}

func TestRenderServerHCL_ExplicitEndpointSpiffeID(t *testing.T) {
	got, err := RenderServerHCL(ServerConfig{
		TrustDomain:           "forge.gcp.local",
		PeerTrustDomain:       "forge.aws.local",
		PeerBundleEndpointURL: "https://10.1.0.10:8443",
		StateMode:             StateModeDisk,
		PeerEndpointSpiffeID:  "spiffe://forge.aws.local/custom/endpoint",
	})
	if err != nil {
		t.Fatalf("RenderServerHCL: %v", err)
	}
	if !strings.Contains(got, "spiffe://forge.aws.local/custom/endpoint") {
		t.Errorf("explicit PeerEndpointSpiffeID not honored:\n%s", got)
	}
}

func TestRenderServerHCL_RejectsUnknownProfile(t *testing.T) {
	_, err := RenderServerHCL(ServerConfig{
		TrustDomain:           "forge.gcp.local",
		PeerTrustDomain:       "forge.aws.local",
		PeerBundleEndpointURL: "https://10.1.0.10:8443",
		StateMode:             StateModeDisk,
		BundleProfile:         "mtls",
	})
	if err == nil {
		t.Fatal("expected error for unknown bundle profile")
	}
}

func TestRenderAgentStartupScript_DoesNotStartWithoutToken(t *testing.T) {
	script := RenderAgentStartupScript("1.11.2", "agent {}")

	// The join token is single-use and minted at bootstrap, so the unit must
	// not start until an operator supplies it.
	for _, want := range []string{
		"ConditionPathExists=/etc/spire/agent-join-token",
		"EnvironmentFile=/etc/spire/agent-join-token",
		"forge-agent-join",
		"umask 077",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("agent script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "systemctl enable --now spire-agent\n\nsystemctl daemon-reload") {
		t.Error("agent must not be started by the startup script itself")
	}
}

func TestRenderAgentStartupScript_VerifiesDownload(t *testing.T) {
	script := RenderAgentStartupScript("1.11.2", "agent {}")
	for _, want := range []string{
		"_sha256sum.txt",
		"sha256sum -c",
		"--proto '=https'",
		"install -m 0755 \"spire-${SPIRE_VERSION}/bin/spire-agent\" /usr/local/bin/spire-agent",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("agent script missing %q:\n%s", want, script)
		}
	}
}

// Both scripts must share one fail-closed download path.
func TestInstallFragment_SharedByServerAndAgent(t *testing.T) {
	server := RenderServerStartupScript("1.11.2", "server {}", "/dev/xvdf")
	agent := RenderAgentStartupScript("1.11.2", "agent {}")
	for _, want := range []string{"sha256sum -c", "--retry-all-errors", "--proto '=https'"} {
		if !strings.Contains(server, want) || !strings.Contains(agent, want) {
			t.Errorf("%q must appear in both server and agent scripts", want)
		}
	}
}
