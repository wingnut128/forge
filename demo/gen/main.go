// Command gen renders the demo SPIRE configs into demo/generated/ from pkg/spire.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wingnut128/forge/pkg/spire"
)

const (
	gcpTD = "forge.gcp.local"
	awsTD = "forge.aws.local"
)

func main() {
	outDir := "demo/generated"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := run(outDir); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	files := map[string]func() (string, error){
		"server-gcp.conf": func() (string, error) {
			return spire.RenderServerHCL(spire.ServerConfig{
				TrustDomain:           gcpTD,
				PeerTrustDomain:       awsTD,
				PeerBundleEndpointURL: "https://spire-aws-server:8443",
				StateMode:             spire.StateModeDisk,
			})
		},
		"server-aws.conf": func() (string, error) {
			return spire.RenderServerHCL(spire.ServerConfig{
				TrustDomain:           awsTD,
				PeerTrustDomain:       gcpTD,
				PeerBundleEndpointURL: "https://spire-gcp-server:8443",
				StateMode:             spire.StateModeDisk,
			})
		},
		"agent-gcp.conf": func() (string, error) {
			return spire.RenderAgentHCL(spire.AgentConfig{
				TrustDomain: gcpTD, ServerAddress: "spire-gcp-server",
				InsecureBootstrap: true, // local demo; Phase 2 pins a trust bundle
			})
		},
		"agent-aws.conf": func() (string, error) {
			return spire.RenderAgentHCL(spire.AgentConfig{
				TrustDomain: awsTD, ServerAddress: "spire-aws-server",
				InsecureBootstrap: true, // local demo; Phase 2 pins a trust bundle
			})
		},
	}

	for name, render := range files {
		content, err := render()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Println("wrote", path)
	}
	return nil
}
