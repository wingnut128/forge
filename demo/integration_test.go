//go:build demo

// Package demo holds the end-to-end federation smoke test. It is gated behind
// the `demo` build tag so the default `go test ./...` stays fast and hermetic.
// Run with: go test -tags demo ./demo/ -run TestFederationProof -v
package demo

import (
	"os/exec"
	"strings"
	"testing"
)

func TestFederationProof(t *testing.T) {
	cmd := exec.Command("./run.sh")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	t.Logf("demo output:\n%s", out)
	if err != nil {
		t.Fatalf("demo run failed: %v", err)
	}
	if !strings.Contains(string(out), "PASS: cross-cloud SVID validated") {
		t.Fatal("expected successful cross-cloud validation in demo output")
	}
}
