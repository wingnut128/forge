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
	_, err := RenderServerHCL(ServerConfig{StateMode: StateModeDisk})
	if err == nil {
		t.Fatal("expected error for missing trust domain, got nil")
	}
}
