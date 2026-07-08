package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_WritesGeneratedConfigs(t *testing.T) {
	outDir := t.TempDir()

	if err := run(outDir); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	wantFiles := map[string]string{
		"server-gcp.conf": gcpTD,
		"server-aws.conf": awsTD,
		"agent-gcp.conf":  gcpTD,
		"agent-aws.conf":  awsTD,
	}
	for name, wantTrustDomain := range wantFiles {
		content, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(content), wantTrustDomain) {
			t.Errorf("%s: expected content to reference trust domain %q, got: %s", name, wantTrustDomain, content)
		}
	}
}

func TestRun_CreatesOutputDir(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "nested", "generated")

	if err := run(outDir); err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	if _, err := os.Stat(outDir); err != nil {
		t.Errorf("expected output dir to be created: %v", err)
	}
}
