package wireguard

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Shape-only fixtures, computed rather than written as literals so they cannot
// be mistaken for real key material by secret scanners.
var (
	testKey     = base64.StdEncoding.EncodeToString(make([]byte, 32))
	testPeerKey = base64.StdEncoding.EncodeToString(bytes32(1))
)

func bytes32(fill byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = fill
	}
	return b
}

func validArgs() ScriptArgs {
	return ScriptArgs{
		PackageManager: APT,
		Address:        GCPTunnelIP + "/30",
		PrivateKey:     testKey,
		PeerPublicKey:  testPeerKey,
		PeerEndpoint:   "203.0.113.10:51820",
		AllowedIPs:     TunnelCIDR + ",10.1.0.0/16",
	}
}

func TestRenderScript_IncludesConfigAndStart(t *testing.T) {
	out, err := RenderScript(validArgs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		"[Interface]",
		"[Peer]",
		"Address = 10.99.0.1/30",
		"ListenPort = 51820",
		"Endpoint = 203.0.113.10:51820",
		"AllowedIPs = 10.99.0.0/30,10.1.0.0/16",
		"PersistentKeepalive = 25",
		"systemctl enable --now wg-quick@wg0",
		"net.ipv4.ip_forward=1",
		"umask 077",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered script missing %q:\n%s", want, out)
		}
	}
}

func TestRenderScript_PackageManagerSelectsInstaller(t *testing.T) {
	apt, err := RenderScript(validArgs())
	if err != nil {
		t.Fatalf("apt: %v", err)
	}
	if !strings.Contains(apt, "apt-get install") {
		t.Errorf("apt script should install via apt-get:\n%s", apt)
	}

	args := validArgs()
	args.PackageManager = DNF
	args.Address = AWSTunnelIP + "/30"
	dnf, err := RenderScript(args)
	if err != nil {
		t.Fatalf("dnf: %v", err)
	}
	if !strings.Contains(dnf, "dnf install -y wireguard-tools") {
		t.Errorf("dnf script should install via dnf:\n%s", dnf)
	}
	if strings.Contains(dnf, "apt-get") {
		t.Errorf("dnf script should not reference apt-get:\n%s", dnf)
	}
}

func TestRenderScript_RejectsIncompleteArgs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ScriptArgs)
	}{
		{"missing address", func(a *ScriptArgs) { a.Address = "" }},
		{"missing private key", func(a *ScriptArgs) { a.PrivateKey = "" }},
		{"missing peer public key", func(a *ScriptArgs) { a.PeerPublicKey = "" }},
		{"missing peer endpoint", func(a *ScriptArgs) { a.PeerEndpoint = "" }},
		{"missing allowed ips", func(a *ScriptArgs) { a.AllowedIPs = "" }},
		{"unknown package manager", func(a *ScriptArgs) { a.PackageManager = "yum" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := validArgs()
			tc.mutate(&args)
			if _, err := RenderScript(args); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// The two ends must not claim the same tunnel address.
func TestTunnelAddressesAreDistinct(t *testing.T) {
	if GCPTunnelIP == AWSTunnelIP {
		t.Fatalf("tunnel endpoints collide: %s", GCPTunnelIP)
	}
}
