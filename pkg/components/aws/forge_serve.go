package aws

import (
	"fmt"
	"strings"
)

// DefaultForgeRepoRef is the git ref built on the AWS SPIRE server VM.
const (
	DefaultForgeRepoRef = "main"
	forgeRepoURL        = "https://github.com/wingnut128/forge.git"
)

// forgeServeArgs configures the `forge serve` validator unit.
type forgeServeArgs struct {
	// LocalTrustDomain is this side's trust domain (AWS).
	LocalTrustDomain string
	// RemoteTrustDomain is the peer's trust domain (GCP).
	RemoteTrustDomain string
	// BundleEndpointURL is the peer's SPIRE bundle endpoint, reached over the
	// WireGuard tunnel.
	BundleEndpointURL string
	// RepoRef is the git ref to build. Defaults to DefaultForgeRepoRef.
	RepoRef string
}

// renderForgeServeScript returns a bash fragment that builds `forge` from
// source and installs it as a systemd unit.
//
// It is a fragment, appended to the SPIRE server's startup script — the
// validator runs alongside the AWS SPIRE server rather than on its own VM,
// which costs nothing extra and is what the demo does in miniature.
//
// The unit restarts on failure by design. pkg/attestation treats the initial
// bundle fetch as fatal, so `forge serve` exits until the peer's bundle
// endpoint is reachable AND the trust bundles have been exchanged. It will
// therefore crash-loop from first boot until the manual bootstrap completes,
// then come up on its own — no operator step is needed to start it.
func renderForgeServeScript(args forgeServeArgs) (string, error) {
	if args.LocalTrustDomain == "" || args.RemoteTrustDomain == "" {
		return "", fmt.Errorf("forge serve: local and remote trust domains are required")
	}
	if args.BundleEndpointURL == "" {
		return "", fmt.Errorf("forge serve: bundle endpoint URL is required")
	}
	ref := args.RepoRef
	if ref == "" {
		ref = DefaultForgeRepoRef
	}

	// Go 1.21+ honours the toolchain line in go.mod and fetches the required
	// version automatically, so the distro package only has to be new enough to
	// bootstrap. Pinning a Go tarball here would go stale instead.
	const tmpl = `
dnf install -y git golang

if [ ! -d /opt/forge ]; then
  git clone --depth 1 --branch __REF__ __REPO__ /opt/forge
fi

if [ ! -x /usr/local/bin/forge ]; then
  cd /opt/forge
  export GOTOOLCHAIN=auto
  export HOME=/root
  go build -o /usr/local/bin/forge ./cmd/forge
fi

cat >/etc/systemd/system/forge-serve.service <<'UNIT'
[Unit]
Description=Forge attestation validator
After=network-online.target
Wants=network-online.target

[Service]
Environment=FORGE_LOCAL_TRUST_DOMAIN=__LOCAL_TD__
Environment=FORGE_REMOTE_TRUST_DOMAIN=__REMOTE_TD__
Environment=FORGE_BUNDLE_ENDPOINT_URL=__BUNDLE_URL__
Environment=FORGE_LISTEN_ADDR=:8080
ExecStart=/usr/local/bin/forge serve
# The initial bundle fetch is fatal, so this crash-loops until the peer
# endpoint is up and the trust bundles have been exchanged. That is expected
# before bootstrap completes.
Restart=always
RestartSec=15

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now forge-serve
`

	r := strings.NewReplacer(
		"__REF__", ref,
		"__REPO__", forgeRepoURL,
		"__LOCAL_TD__", args.LocalTrustDomain,
		"__REMOTE_TD__", args.RemoteTrustDomain,
		"__BUNDLE_URL__", args.BundleEndpointURL,
	)
	return r.Replace(tmpl), nil
}
