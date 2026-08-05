// Package wireguard renders the boot-time configuration for the cross-cloud
// tunnel that carries SPIRE federation traffic.
//
// This is deliberately a thin, self-contained transport layer: the SPIRE
// components know nothing about it, so it can be replaced (by a Bowtie mesh,
// for instance) without touching them.
package wireguard

import (
	"fmt"
	"strings"
)

// Tunnel addressing. The /30 carries only the two gateways.
const (
	TunnelCIDR  = "10.99.0.0/30"
	GCPTunnelIP = "10.99.0.1"
	AWSTunnelIP = "10.99.0.2"
	ListenPort  = 51820
)

// PackageManager selects the install command for the host distro.
type PackageManager string

const (
	// APT is Debian/Ubuntu, used by the GCP gateway.
	APT PackageManager = "apt"
	// DNF is Amazon Linux 2023, used by the AWS NAT instance.
	DNF PackageManager = "dnf"
)

// ScriptArgs are the inputs to RenderScript.
type ScriptArgs struct {
	PackageManager PackageManager
	// Address is this end's tunnel address in CIDR form, e.g. "10.99.0.1/30".
	Address       string
	PrivateKey    string
	PeerPublicKey string
	// PeerEndpoint is "host:port" for the remote gateway.
	PeerEndpoint string
	// AllowedIPs lists the CIDRs routed through the tunnel, comma-separated.
	AllowedIPs string
}

// Validate reports whether the arguments are complete enough to render.
func (a ScriptArgs) Validate() error {
	for _, f := range []struct{ name, value string }{
		{"Address", a.Address},
		{"PrivateKey", a.PrivateKey},
		{"PeerPublicKey", a.PeerPublicKey},
		{"PeerEndpoint", a.PeerEndpoint},
		{"AllowedIPs", a.AllowedIPs},
	} {
		if f.value == "" {
			return fmt.Errorf("wireguard: %s is required", f.name)
		}
	}
	if a.PackageManager != APT && a.PackageManager != DNF {
		return fmt.Errorf("wireguard: unsupported package manager %q", a.PackageManager)
	}
	return nil
}

// RenderScript returns a bash fragment that installs WireGuard, enables IP
// forwarding, writes wg0.conf, and starts the tunnel.
//
// It is a fragment rather than a whole script so it can be appended to an
// existing startup script (the AWS NAT instance already has one). Callers are
// responsible for the shebang.
//
// The config is written with umask 077 because it contains the private key;
// wg-quick refuses to load a world-readable config.
func RenderScript(args ScriptArgs) (string, error) {
	if err := args.Validate(); err != nil {
		return "", err
	}

	var install string
	switch args.PackageManager {
	case APT:
		install = "export DEBIAN_FRONTEND=noninteractive\n" +
			"apt-get update -qq\n" +
			"apt-get install -y -qq wireguard\n"
	case DNF:
		install = "dnf install -y wireguard-tools\n"
	}

	// PersistentKeepalive keeps the mapping alive through both clouds' NAT.
	return fmt.Sprintf(`
%s
sysctl -w net.ipv4.ip_forward=1
echo 'net.ipv4.ip_forward=1' >/etc/sysctl.d/99-forge-wg.conf

mkdir -p /etc/wireguard
umask 077
cat >/etc/wireguard/wg0.conf <<'WGCONF'
[Interface]
Address = %s
ListenPort = %d
PrivateKey = %s

[Peer]
PublicKey = %s
AllowedIPs = %s
Endpoint = %s
PersistentKeepalive = 25
WGCONF

systemctl enable --now wg-quick@wg0
`, install, args.Address, ListenPort, args.PrivateKey,
		args.PeerPublicKey, strings.TrimSpace(args.AllowedIPs), args.PeerEndpoint), nil
}
