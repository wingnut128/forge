# SPIRE Bootstrap — Phase 1 Local Federation Proof — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the cross-cloud SPIFFE/SPIRE trust model end-to-end locally: two federated SPIRE servers (GCP/AWS roles), a JWT-SVID minted on the GCP side validates on the AWS side through Forge's existing `pkg/attestation` path — runnable with one `make demo`.

**Architecture:** A new `pkg/spire` package renders federation-aware SPIRE config (pure functions, golden-tested). Both the existing VM startup scripts and a new `demo/` harness consume it. The demo runs SPIRE on Apple `container` (Docker fallback), serves each server's native RFC 9409 bundle endpoint over `https_web` with a throwaway demo CA, and validates a minted SVID via `forge serve`. Trust in the demo CA is injected via `SSL_CERT_FILE` — no custom images, no runtime code changes.

**Tech Stack:** Go 1.25, `text/template`, go-spiffe v2.6.0, SPIRE 1.11.2 (upstream `ghcr.io/spiffe/spire-server`/`spire-agent` images), Apple `container` CLI / Docker, openssl, bash.

**Spec:** `docs/superpowers/specs/2026-06-15-spire-bootstrap-local-proof-design.md`

---

## File Structure

**Create:**
- `pkg/spire/config.go` — `ServerConfig`, `AgentConfig`, `RenderServerHCL`, `RenderAgentHCL` (pure, no I/O)
- `pkg/spire/config_test.go` — golden tests
- `pkg/spire/testdata/server_gcp_disk.golden`
- `pkg/spire/testdata/server_aws_managed.golden`
- `pkg/spire/testdata/agent_gcp.golden`
- `demo/gen/main.go` — renders config files into `demo/generated/` from `pkg/spire`
- `demo/gen-certs.sh` — demo CA + per-server `https_web` certs
- `demo/bootstrap.sh` — federation bundle exchange, registration entry, join token, mint, validate
- `demo/run.sh` — Apple `container` orchestration (+ Docker fallback dispatch)
- `demo/docker-compose.yml` — Docker fallback
- `demo/validate.sh` — assertion helper used by bootstrap + integration test
- `demo/integration_test.go` — `//go:build demo` smoke test
- `docs/why-this-model.md` — the "why it's a valid model" narrative

**Modify:**
- `pkg/components/gcp/spire_server.go` — render server config via `pkg/spire`
- `pkg/components/aws/spire_server.go` — render server config via `pkg/spire`
- `Makefile` — add `demo` + `demo-clean` targets
- `README.md`, `CLAUDE.md`, `TODO.md` — document the demo + new package

---

## Task 1: `pkg/spire` — ServerConfig + RenderServerHCL (disk mode)

**Files:**
- Create: `pkg/spire/config.go`
- Create: `pkg/spire/config_test.go`
- Create: `pkg/spire/testdata/server_gcp_disk.golden`

- [ ] **Step 1: Write the golden file** `pkg/spire/testdata/server_gcp_disk.golden`

```hcl
server {
    bind_address = "0.0.0.0"
    bind_port = "8081"
    trust_domain = "forge.gcp.local"
    data_dir = "/var/lib/spire/data"
    log_level = "INFO"

    federation {
        bundle_endpoint {
            address = "0.0.0.0"
            port = 8443
            profile "https_web" {
                serving_cert_file {
                    cert_file_path = "/etc/spire/certs/server.crt"
                    key_file_path = "/etc/spire/certs/server.key"
                }
            }
        }

        federates_with "forge.aws.local" {
            bundle_endpoint_url = "https://spire-aws-server:8443"
            bundle_endpoint_profile "https_web" {}
        }
    }
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "/var/lib/spire/data/datastore.sqlite3"
        }
    }

    KeyManager "disk" {
        plugin_data {
            keys_path = "/var/lib/spire/data/keys.json"
        }
    }

    NodeAttestor "join_token" {
        plugin_data {}
    }
}
```

- [ ] **Step 2: Write the failing test** `pkg/spire/config_test.go`

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/spire/ -run TestRenderServerHCL -v`
Expected: FAIL — `undefined: ServerConfig` / `RenderServerHCL`.

- [ ] **Step 4: Write minimal implementation** `pkg/spire/config.go`

```go
// Package spire renders federation-aware SPIRE server and agent configuration.
// Pure functions, no I/O — both the VM startup scripts and the local demo
// harness render config from here so they can never drift.
package spire

import (
	"fmt"
	"strings"
	"text/template"
)

// StateMode selects the SPIRE DataStore backing store.
type StateMode string

const (
	// StateModeDisk uses sqlite3 on the local data disk (default cheap track).
	StateModeDisk StateMode = "disk"
	// StateModeManaged uses a managed Postgres DataStore (Cloud SQL / RDS).
	StateModeManaged StateMode = "managed"
)

// ServerConfig is the input for RenderServerHCL.
type ServerConfig struct {
	TrustDomain string // required, e.g. "forge.gcp.local"
	DataDir     string // default "/var/lib/spire/data"
	BindAddress string // default "0.0.0.0"
	BindPort    string // default "8081"
	LogLevel    string // default "INFO"
	StateMode   StateMode

	// Federation
	PeerTrustDomain       string // required
	PeerBundleEndpointURL string // required, e.g. "https://spire-aws-server:8443"
	BundleEndpointAddress string // default "0.0.0.0"
	BundleEndpointPort    string // default "8443"
	BundleEndpointCert    string // default "/etc/spire/certs/server.crt"
	BundleEndpointKey     string // default "/etc/spire/certs/server.key"

	// ManagedDBConnString is the Postgres connection string used when
	// StateMode is StateModeManaged. Ignored for disk mode.
	ManagedDBConnString string
}

type serverTemplateData struct {
	ServerConfig
	DBType     string
	ConnString string
}

var serverTemplate = template.Must(template.New("server").Parse(
	`server {
    bind_address = "{{.BindAddress}}"
    bind_port = "{{.BindPort}}"
    trust_domain = "{{.TrustDomain}}"
    data_dir = "{{.DataDir}}"
    log_level = "{{.LogLevel}}"

    federation {
        bundle_endpoint {
            address = "{{.BundleEndpointAddress}}"
            port = {{.BundleEndpointPort}}
            profile "https_web" {
                serving_cert_file {
                    cert_file_path = "{{.BundleEndpointCert}}"
                    key_file_path = "{{.BundleEndpointKey}}"
                }
            }
        }

        federates_with "{{.PeerTrustDomain}}" {
            bundle_endpoint_url = "{{.PeerBundleEndpointURL}}"
            bundle_endpoint_profile "https_web" {}
        }
    }
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "{{.DBType}}"
            connection_string = "{{.ConnString}}"
        }
    }

    KeyManager "disk" {
        plugin_data {
            keys_path = "{{.DataDir}}/keys.json"
        }
    }

    NodeAttestor "join_token" {
        plugin_data {}
    }
}
`))

// RenderServerHCL renders a federation-aware SPIRE server.conf.
func RenderServerHCL(cfg ServerConfig) (string, error) {
	if cfg.TrustDomain == "" {
		return "", fmt.Errorf("spire: TrustDomain is required")
	}
	if cfg.PeerTrustDomain == "" {
		return "", fmt.Errorf("spire: PeerTrustDomain is required")
	}
	if cfg.PeerBundleEndpointURL == "" {
		return "", fmt.Errorf("spire: PeerBundleEndpointURL is required")
	}
	applyServerDefaults(&cfg)

	data := serverTemplateData{ServerConfig: cfg}
	switch cfg.StateMode {
	case StateModeManaged:
		if cfg.ManagedDBConnString == "" {
			return "", fmt.Errorf("spire: ManagedDBConnString is required for managed mode")
		}
		data.DBType = "postgres"
		data.ConnString = cfg.ManagedDBConnString
	default: // disk
		data.DBType = "sqlite3"
		data.ConnString = cfg.DataDir + "/datastore.sqlite3"
	}

	var sb strings.Builder
	if err := serverTemplate.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("spire: rendering server config: %w", err)
	}
	return sb.String(), nil
}

func applyServerDefaults(cfg *ServerConfig) {
	if cfg.DataDir == "" {
		cfg.DataDir = "/var/lib/spire/data"
	}
	if cfg.BindAddress == "" {
		cfg.BindAddress = "0.0.0.0"
	}
	if cfg.BindPort == "" {
		cfg.BindPort = "8081"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}
	if cfg.StateMode == "" {
		cfg.StateMode = StateModeDisk
	}
	if cfg.BundleEndpointAddress == "" {
		cfg.BundleEndpointAddress = "0.0.0.0"
	}
	if cfg.BundleEndpointPort == "" {
		cfg.BundleEndpointPort = "8443"
	}
	if cfg.BundleEndpointCert == "" {
		cfg.BundleEndpointCert = "/etc/spire/certs/server.crt"
	}
	if cfg.BundleEndpointKey == "" {
		cfg.BundleEndpointKey = "/etc/spire/certs/server.key"
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/spire/ -run TestRenderServerHCL -v`
Expected: PASS (both subtests). If the golden mismatches on whitespace, fix the golden file to match the renderer byte-for-byte.

- [ ] **Step 6: Commit**

```bash
git add pkg/spire/config.go pkg/spire/config_test.go pkg/spire/testdata/server_gcp_disk.golden
git commit -m "feat(spire): render federation-aware server config (disk mode)"
```

---

## Task 2: RenderServerHCL — managed-state branch

**Files:**
- Modify: `pkg/spire/config_test.go`
- Create: `pkg/spire/testdata/server_aws_managed.golden`

- [ ] **Step 1: Write the golden file** `pkg/spire/testdata/server_aws_managed.golden`

(Identical structure to the disk golden but trust domains swapped and the DataStore block is Postgres. Full file:)

```hcl
server {
    bind_address = "0.0.0.0"
    bind_port = "8081"
    trust_domain = "forge.aws.local"
    data_dir = "/var/lib/spire/data"
    log_level = "INFO"

    federation {
        bundle_endpoint {
            address = "0.0.0.0"
            port = 8443
            profile "https_web" {
                serving_cert_file {
                    cert_file_path = "/etc/spire/certs/server.crt"
                    key_file_path = "/etc/spire/certs/server.key"
                }
            }
        }

        federates_with "forge.gcp.local" {
            bundle_endpoint_url = "https://spire-gcp-server:8443"
            bundle_endpoint_profile "https_web" {}
        }
    }
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "postgres"
            connection_string = "postgres://spire@db.example:5432/spire"
        }
    }

    KeyManager "disk" {
        plugin_data {
            keys_path = "/var/lib/spire/data/keys.json"
        }
    }

    NodeAttestor "join_token" {
        plugin_data {}
    }
}
```

- [ ] **Step 2: Add the failing test** to `pkg/spire/config_test.go`

```go
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
```

- [ ] **Step 3: Run test to verify it passes** (the implementation from Task 1 already handles managed mode)

Run: `go test ./pkg/spire/ -run TestRenderServerHCL -v`
Expected: PASS for all four subtests. (If `TestRenderServerHCL_AWSManaged` fails on whitespace, align the golden to the renderer output.)

- [ ] **Step 4: Commit**

```bash
git add pkg/spire/config_test.go pkg/spire/testdata/server_aws_managed.golden
git commit -m "test(spire): cover managed-state server config branch"
```

---

## Task 3: RenderAgentHCL

**Files:**
- Modify: `pkg/spire/config.go`
- Modify: `pkg/spire/config_test.go`
- Create: `pkg/spire/testdata/agent_gcp.golden`

- [ ] **Step 1: Write the golden file** `pkg/spire/testdata/agent_gcp.golden`

```hcl
agent {
    data_dir = "/var/lib/spire/agent"
    log_level = "INFO"
    trust_domain = "forge.gcp.local"
    server_address = "spire-gcp-server"
    server_port = "8081"
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }

    KeyManager "disk" {
        plugin_data {
            directory = "/var/lib/spire/agent"
        }
    }

    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
```

- [ ] **Step 2: Write the failing test** in `pkg/spire/config_test.go`

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/spire/ -run TestRenderAgentHCL -v`
Expected: FAIL — `undefined: AgentConfig` / `RenderAgentHCL`.

- [ ] **Step 4: Add the implementation** to `pkg/spire/config.go`

```go
// AgentConfig is the input for RenderAgentHCL.
type AgentConfig struct {
	TrustDomain   string // required
	ServerAddress string // required, e.g. "spire-gcp-server"
	ServerPort    string // default "8081"
	DataDir       string // default "/var/lib/spire/agent"
	LogLevel      string // default "INFO"
}

var agentTemplate = template.Must(template.New("agent").Parse(
	`agent {
    data_dir = "{{.DataDir}}"
    log_level = "{{.LogLevel}}"
    trust_domain = "{{.TrustDomain}}"
    server_address = "{{.ServerAddress}}"
    server_port = "{{.ServerPort}}"
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }

    KeyManager "disk" {
        plugin_data {
            directory = "{{.DataDir}}"
        }
    }

    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
`))

// RenderAgentHCL renders a SPIRE agent.conf using join-token node attestation.
func RenderAgentHCL(cfg AgentConfig) (string, error) {
	if cfg.TrustDomain == "" {
		return "", fmt.Errorf("spire: TrustDomain is required")
	}
	if cfg.ServerAddress == "" {
		return "", fmt.Errorf("spire: ServerAddress is required")
	}
	if cfg.ServerPort == "" {
		cfg.ServerPort = "8081"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "/var/lib/spire/agent"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}

	var sb strings.Builder
	if err := agentTemplate.Execute(&sb, cfg); err != nil {
		return "", fmt.Errorf("spire: rendering agent config: %w", err)
	}
	return sb.String(), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/spire/ -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Commit**

```bash
git add pkg/spire/config.go pkg/spire/config_test.go pkg/spire/testdata/agent_gcp.golden
git commit -m "feat(spire): render agent config with join-token attestation"
```

---

## Task 4: Refactor GCP SPIRE startup script to use `pkg/spire`

**Files:**
- Modify: `pkg/components/gcp/spire_server.go` (the `spireGCPStartupScript` function)

Background: today `spireGCPStartupScript` embeds a placeholder `server.conf` heredoc. Replace just the config-generation with `spire.RenderServerHCL`. The disk-mount / binary-install / systemd parts stay. The peer bundle endpoint URL is derived as a Phase-2 placeholder (`https://<peer-td>:8443`); live peer wiring is Phase 2.

- [ ] **Step 1: Add the import** at the top of `pkg/components/gcp/spire_server.go`

```go
import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/wingnut128/forge/pkg/spire"
)
```

- [ ] **Step 2: Replace the body of `spireGCPStartupScript`**

Replace the entire existing `func spireGCPStartupScript(args *SPIREServerArgs) string { ... }` with:

```go
func spireGCPStartupScript(args *SPIREServerArgs) string {
	mode := spire.StateModeDisk
	if args.ManagedStateMode {
		mode = spire.StateModeManaged
	}
	// Phase 2 wires the real peer bundle endpoint (peer SPIRE server IP).
	// For now derive a placeholder so config renders; live VMs are not yet
	// exercised end-to-end (see specs/2026-06-15-spire-bootstrap-local-proof).
	serverHCL, err := spire.RenderServerHCL(spire.ServerConfig{
		TrustDomain:           args.TrustDomain,
		PeerTrustDomain:       args.PeerTrustDomain,
		PeerBundleEndpointURL: fmt.Sprintf("https://%s:8443", args.PeerTrustDomain),
		StateMode:             mode,
		ManagedDBConnString:   "postgres://spire@127.0.0.1:5432/spire", // Phase 2: real managed DSN
	})
	if err != nil {
		// Render only fails on missing required fields; surface as a config error
		// baked into the script so the VM logs make the cause obvious.
		serverHCL = "# ERROR rendering SPIRE config: " + err.Error()
	}

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
SPIRE_VERSION=%q

mkdir -p /var/lib/spire
if ! mountpoint -q /var/lib/spire; then
  DEV=/dev/disk/by-id/google-spire-data
  if ! blkid "$DEV" >/dev/null 2>&1; then
    mkfs.ext4 -F "$DEV"
  fi
  echo "$DEV /var/lib/spire ext4 defaults,nofail 0 2" >> /etc/fstab
  mount /var/lib/spire
fi

if [ ! -x /usr/local/bin/spire-server ]; then
  cd /tmp
  curl -sSL -o spire.tar.gz "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz"
  tar -xzf spire.tar.gz
  install -m 0755 spire-${SPIRE_VERSION}/bin/spire-server /usr/local/bin/spire-server
fi

mkdir -p /etc/spire /etc/spire/certs /var/lib/spire/data
cat >/etc/spire/server.conf <<'CONF'
%s
CONF

cat >/etc/systemd/system/spire-server.service <<UNIT
[Unit]
Description=SPIRE Server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/spire-server run -config /etc/spire/server.conf
Restart=always

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now spire-server
`, args.SPIREVersion, serverHCL)
}
```

Note: the config heredoc delimiter is now quoted (`<<'CONF'`) so the rendered HCL is written literally (no shell expansion of `$`-containing values).

- [ ] **Step 3: Build and vet**

Run: `go build ./... && go vet ./pkg/components/gcp/`
Expected: no errors.

- [ ] **Step 4: Run the full test suite** (ensure no regressions)

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/components/gcp/spire_server.go
git commit -m "refactor(gcp): render SPIRE server config via pkg/spire"
```

---

## Task 5: Refactor AWS SPIRE startup script to use `pkg/spire`

**Files:**
- Modify: `pkg/components/aws/spire_server.go` (the `spireAWSUserData` function)

- [ ] **Step 1: Add the import** at the top of `pkg/components/aws/spire_server.go`

```go
import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/wingnut128/forge/pkg/spire"
)
```

- [ ] **Step 2: Replace the body of `spireAWSUserData`**

Replace the entire existing `func spireAWSUserData(args *SPIREServerArgs) string { ... }` with:

```go
func spireAWSUserData(args *SPIREServerArgs) string {
	mode := spire.StateModeDisk
	if args.ManagedStateMode {
		mode = spire.StateModeManaged
	}
	serverHCL, err := spire.RenderServerHCL(spire.ServerConfig{
		TrustDomain:           args.TrustDomain,
		PeerTrustDomain:       args.PeerTrustDomain,
		PeerBundleEndpointURL: fmt.Sprintf("https://%s:8443", args.PeerTrustDomain),
		StateMode:             mode,
		ManagedDBConnString:   "postgres://spire@127.0.0.1:5432/spire", // Phase 2: real managed DSN
	})
	if err != nil {
		serverHCL = "# ERROR rendering SPIRE config: " + err.Error()
	}

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
SPIRE_VERSION=%q

DEV=/dev/xvdf
mkdir -p /var/lib/spire
if ! blkid "$DEV" >/dev/null 2>&1; then
  mkfs.ext4 -F "$DEV"
fi
if ! mountpoint -q /var/lib/spire; then
  echo "$DEV /var/lib/spire ext4 defaults,nofail 0 2" >> /etc/fstab
  mount /var/lib/spire
fi

if [ ! -x /usr/local/bin/spire-server ]; then
  cd /tmp
  curl -sSL -o spire.tar.gz "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz"
  tar -xzf spire.tar.gz
  install -m 0755 spire-${SPIRE_VERSION}/bin/spire-server /usr/local/bin/spire-server
fi

mkdir -p /etc/spire /etc/spire/certs /var/lib/spire/data
cat >/etc/spire/server.conf <<'CONF'
%s
CONF

cat >/etc/systemd/system/spire-server.service <<UNIT
[Unit]
Description=SPIRE Server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/spire-server run -config /etc/spire/server.conf
Restart=always

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now spire-server
`, args.SPIREVersion, serverHCL)
}
```

- [ ] **Step 3: Build, vet, test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/components/aws/spire_server.go
git commit -m "refactor(aws): render SPIRE server config via pkg/spire"
```

---

## Task 6: `demo/gen` — render config files from `pkg/spire`

**Files:**
- Create: `demo/gen/main.go`

This writes the four config files the containers mount, so the demo never hand-edits HCL.

- [ ] **Step 1: Write the generator** `demo/gen/main.go`

```go
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
			})
		},
		"agent-aws.conf": func() (string, error) {
			return spire.RenderAgentHCL(spire.AgentConfig{
				TrustDomain: awsTD, ServerAddress: "spire-aws-server",
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
```

- [ ] **Step 2: Run it and verify output**

Run: `go run ./demo/gen demo/generated && ls demo/generated`
Expected: prints four `wrote …` lines; `server-gcp.conf server-aws.conf agent-gcp.conf agent-aws.conf` exist.

- [ ] **Step 3: Verify a rendered file matches the golden**

Run: `diff <(go run ./demo/gen /tmp/spiregen >/dev/null; cat /tmp/spiregen/server-gcp.conf) pkg/spire/testdata/server_gcp_disk.golden`
Expected: no diff.

- [ ] **Step 4: Ignore generated output**

Append to `.gitignore`:

```
demo/generated/
demo/certs/
```

- [ ] **Step 5: Commit**

```bash
git add demo/gen/main.go .gitignore
git commit -m "feat(demo): render SPIRE configs from pkg/spire"
```

---

## Task 7: `demo/gen-certs.sh` — demo CA + https_web serving certs

**Files:**
- Create: `demo/gen-certs.sh`

Generates a throwaway CA and one server cert per SPIRE server (SAN = its network hostname). The CA is later mounted into every container and trusted via `SSL_CERT_FILE`.

- [ ] **Step 1: Write the script** `demo/gen-certs.sh`

```bash
#!/usr/bin/env bash
# Generate a throwaway demo CA and https_web serving certs for the SPIRE
# bundle endpoints. Output: demo/certs/{ca.crt,ca.key,<host>.crt,<host>.key}.
set -euo pipefail

CERT_DIR="${1:-demo/certs}"
mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

HOSTS=(spire-gcp-server spire-aws-server)

if [ ! -f ca.crt ]; then
  openssl genrsa -out ca.key 2048
  openssl req -x509 -new -nodes -key ca.key -sha256 -days 7 \
    -subj "/CN=forge-demo-ca" -out ca.crt
fi

for host in "${HOSTS[@]}"; do
  [ -f "${host}.crt" ] && continue
  openssl genrsa -out "${host}.key" 2048
  openssl req -new -key "${host}.key" -subj "/CN=${host}" -out "${host}.csr"
  cat > "${host}.ext" <<EXT
subjectAltName = DNS:${host}
extendedKeyUsage = serverAuth
EXT
  openssl x509 -req -in "${host}.csr" -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out "${host}.crt" -days 7 -sha256 \
    -extfile "${host}.ext"
  rm -f "${host}.csr" "${host}.ext"
  echo "issued ${host}.crt"
done

echo "demo CA + serving certs ready in $CERT_DIR"
```

- [ ] **Step 2: Make executable and run**

Run: `chmod +x demo/gen-certs.sh && ./demo/gen-certs.sh demo/certs && ls demo/certs`
Expected: `ca.crt ca.key spire-gcp-server.crt spire-gcp-server.key spire-aws-server.crt spire-aws-server.key`.

- [ ] **Step 3: Verify SAN is correct**

Run: `openssl x509 -in demo/certs/spire-gcp-server.crt -noout -text | grep -A1 "Subject Alternative Name"`
Expected: shows `DNS:spire-gcp-server`.

- [ ] **Step 4: Commit**

```bash
git add demo/gen-certs.sh
git commit -m "feat(demo): generate demo CA and https_web serving certs"
```

---

## Task 8: `demo/bootstrap.sh` — federation bootstrap + mint + validate

**Files:**
- Create: `demo/bootstrap.sh`
- Create: `demo/validate.sh`

`bootstrap.sh` is runtime-agnostic: it takes the container-exec command as `$EXEC` (`container exec` or `docker exec`) so `run.sh` can pass either. This is the realization of the long-standing "post-provision SPIRE bootstrap" TODO.

- [ ] **Step 1: Write** `demo/validate.sh`

```bash
#!/usr/bin/env bash
# POST a token to forge serve /validate and assert valid:true with the
# expected remote trust domain. Usage: validate.sh <forge-serve-url> <token> <expected-td>
set -euo pipefail

URL="$1"
TOKEN="$2"
EXPECT_TD="$3"

resp="$(curl -sS -X POST "$URL/validate" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"${TOKEN}\"}")"

echo "validate response: $resp"

echo "$resp" | grep -q '"valid":true' || { echo "FAIL: token not valid"; exit 1; }
echo "$resp" | grep -q "\"trust_domain\":\"${EXPECT_TD}\"" || {
  echo "FAIL: trust domain mismatch (want ${EXPECT_TD})"; exit 1; }

echo "PASS: cross-cloud SVID validated (remote td=${EXPECT_TD})"
```

- [ ] **Step 2: Write** `demo/bootstrap.sh`

```bash
#!/usr/bin/env bash
# Bootstrap cross-cloud SPIRE federation, mint a JWT-SVID on the GCP side,
# and validate it through forge serve (AWS side).
# Requires: EXEC env var = container-exec command (e.g. "container exec" or "docker exec").
set -euo pipefail

EXEC="${EXEC:?set EXEC to the container exec command}"
GCP_SRV=spire-gcp-server
AWS_SRV=spire-aws-server
GCP_AGENT=spire-gcp-agent
GCP_TD=forge.gcp.local
AWS_TD=forge.aws.local
WORKLOAD_ID="spiffe://${GCP_TD}/workload/demo"
FORGE_URL="http://localhost:8080"

srv() { $EXEC "$1" /opt/spire/bin/spire-server "${@:2}"; }

echo "==> waiting for SPIRE servers to be healthy"
for s in "$GCP_SRV" "$AWS_SRV"; do
  for i in $(seq 1 30); do
    if srv "$s" healthcheck >/dev/null 2>&1; then echo "  $s healthy"; break; fi
    sleep 2
    [ "$i" = 30 ] && { echo "FAIL: $s never became healthy"; exit 1; }
  done
done

echo "==> exchanging trust bundles (federation)"
# GCP bundle -> AWS server (federates_with GCP)
srv "$GCP_SRV" bundle show -format spiffe > /tmp/gcp.bundle
$EXEC -i "$AWS_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${GCP_TD}" < /tmp/gcp.bundle
# AWS bundle -> GCP server (federates_with AWS)
srv "$AWS_SRV" bundle show -format spiffe > /tmp/aws.bundle
$EXEC -i "$GCP_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${AWS_TD}" < /tmp/aws.bundle

echo "==> creating join token + registering GCP agent"
JOIN_TOKEN="$(srv "$GCP_SRV" token generate -spiffeID "spiffe://${GCP_TD}/agent" \
  | sed -n 's/^Token: //p')"
[ -n "$JOIN_TOKEN" ] || { echo "FAIL: empty join token"; exit 1; }

# Start the agent with the join token (agent container already running, idle).
$EXEC "$GCP_AGENT" sh -c \
  "/opt/spire/bin/spire-agent run -config /etc/spire/agent.conf -joinToken ${JOIN_TOKEN} &
   sleep 5"

echo "==> registering demo workload (federated with AWS)"
srv "$GCP_SRV" entry create \
  -parentID "spiffe://${GCP_TD}/agent" \
  -spiffeID "${WORKLOAD_ID}" \
  -selector "unix:uid:0" \
  -federatesWith "${AWS_TD}"
sleep 5

echo "==> minting JWT-SVID on GCP (audience = AWS trust domain)"
TOKEN="$($EXEC "$GCP_AGENT" /opt/spire/bin/spire-agent api fetch jwt \
  -audience "${AWS_TD}" -spiffeID "${WORKLOAD_ID}" \
  -socketPath /tmp/agent.sock | sed -n '2p' | tr -d '[:space:]')"
[ -n "$TOKEN" ] || { echo "FAIL: empty JWT-SVID"; exit 1; }

echo "==> validating the SVID through forge serve (AWS role)"
bash demo/validate.sh "$FORGE_URL" "$TOKEN" "$GCP_TD"
```

Note for the implementer: the exact stdout shape of `spire-agent api fetch jwt` (which line holds the token) and `token generate` (the `Token:` prefix) should be confirmed against SPIRE 1.11.2 on first run and the `sed`/`grep` adjusted if needed — the script fails loudly (empty-token guard) if parsing is off.

- [ ] **Step 3: Make executable**

Run: `chmod +x demo/bootstrap.sh demo/validate.sh`
Expected: no output.

- [ ] **Step 4: Shellcheck (if available) and commit**

Run: `command -v shellcheck >/dev/null && shellcheck demo/bootstrap.sh demo/validate.sh || echo "shellcheck not installed, skipping"`
Expected: no errors (or skip notice).

```bash
git add demo/bootstrap.sh demo/validate.sh
git commit -m "feat(demo): bootstrap federation, mint SVID, validate cross-cloud"
```

---

## Task 9: `demo/run.sh` (Apple container) + Docker Compose fallback

**Files:**
- Create: `demo/run.sh`
- Create: `demo/docker-compose.yml`

Both bring up: 2 SPIRE servers, 2 agents (idle until bootstrap starts them), and `forge-serve`. SPIRE images: `ghcr.io/spiffe/spire-server:1.11.2`, `ghcr.io/spiffe/spire-agent:1.11.2`. `forge-serve` runs a host-built static linux binary in `alpine`. The demo CA is mounted and trusted via `SSL_CERT_FILE`.

- [ ] **Step 1: Write** `demo/run.sh`

```bash
#!/usr/bin/env bash
# Bring up the local cross-cloud federation demo on Apple `container`
# (default) or Docker (DEMO_RUNTIME=docker), then run bootstrap.sh.
set -euo pipefail

RT="${DEMO_RUNTIME:-container}"   # container | docker
NET=forge-demo
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GEN="$ROOT/demo/generated"
CERTS="$ROOT/demo/certs"
SPIRE_SERVER_IMG=ghcr.io/spiffe/spire-server:1.11.2
SPIRE_AGENT_IMG=ghcr.io/spiffe/spire-agent:1.11.2

cd "$ROOT"

if [ "$RT" = "docker" ]; then
  echo "==> using Docker Compose fallback"
  docker compose -f demo/docker-compose.yml up -d
  EXEC="docker exec" bash demo/bootstrap.sh
  exit $?
fi

echo "==> rendering configs + certs"
go run ./demo/gen "$GEN"
./demo/gen-certs.sh "$CERTS"

echo "==> building forge linux binary"
CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" \
  go build -o demo/generated/forge ./cmd/forge

echo "==> (re)creating network $NET"
container network rm "$NET" >/dev/null 2>&1 || true
container network create "$NET"

run_srv() { # name conf cert key
  container run -d --name "$1" --network "$NET" \
    -v "$GEN/$2:/etc/spire/server.conf:ro" \
    -v "$CERTS/$3:/etc/spire/certs/server.crt:ro" \
    -v "$CERTS/$4:/etc/spire/certs/server.key:ro" \
    -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
    -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
    "$SPIRE_SERVER_IMG" run -config /etc/spire/server.conf
}
run_agent() { # name conf
  container run -d --name "$1" --network "$NET" \
    -v "$GEN/$2:/etc/spire/agent.conf:ro" \
    -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
    -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
    --entrypoint sleep "$SPIRE_AGENT_IMG" infinity
}

echo "==> starting SPIRE servers + agents"
run_srv spire-gcp-server server-gcp.conf spire-gcp-server.crt spire-gcp-server.key
run_srv spire-aws-server server-aws.conf spire-aws-server.crt spire-aws-server.key
run_agent spire-gcp-agent agent-gcp.conf
run_agent spire-aws-agent agent-aws.conf

echo "==> starting forge serve (AWS role)"
container run -d --name forge-serve --network "$NET" -p 8080:8080 \
  -v "$GEN/forge:/usr/local/bin/forge:ro" \
  -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
  -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
  -e FORGE_LOCAL_TRUST_DOMAIN=forge.aws.local \
  -e FORGE_REMOTE_TRUST_DOMAIN=forge.gcp.local \
  -e FORGE_BUNDLE_ENDPOINT_URL=https://spire-gcp-server:8443 \
  -e FORGE_LISTEN_ADDR=:8080 \
  alpine /usr/local/bin/forge serve

echo "==> running bootstrap"
EXEC="container exec" bash demo/bootstrap.sh
```

- [ ] **Step 2: Write** `demo/docker-compose.yml`

```yaml
# Docker fallback for the local federation demo. Apple `container` is primary
# (see demo/run.sh). Requires `go run ./demo/gen demo/generated`,
# `./demo/gen-certs.sh demo/certs`, and a host-built demo/generated/forge first
# (run.sh DEMO_RUNTIME=docker does all of this).
services:
  spire-gcp-server:
    image: ghcr.io/spiffe/spire-server:1.11.2
    command: ["run", "-config", "/etc/spire/server.conf"]
    environment: [SSL_CERT_FILE=/etc/spire/certs/ca.crt]
    volumes:
      - ./generated/server-gcp.conf:/etc/spire/server.conf:ro
      - ./certs/spire-gcp-server.crt:/etc/spire/certs/server.crt:ro
      - ./certs/spire-gcp-server.key:/etc/spire/certs/server.key:ro
      - ./certs/ca.crt:/etc/spire/certs/ca.crt:ro
  spire-aws-server:
    image: ghcr.io/spiffe/spire-server:1.11.2
    command: ["run", "-config", "/etc/spire/server.conf"]
    environment: [SSL_CERT_FILE=/etc/spire/certs/ca.crt]
    volumes:
      - ./generated/server-aws.conf:/etc/spire/server.conf:ro
      - ./certs/spire-aws-server.crt:/etc/spire/certs/server.crt:ro
      - ./certs/spire-aws-server.key:/etc/spire/certs/server.key:ro
      - ./certs/ca.crt:/etc/spire/certs/ca.crt:ro
  spire-gcp-agent:
    image: ghcr.io/spiffe/spire-agent:1.11.2
    entrypoint: ["sleep", "infinity"]
    environment: [SSL_CERT_FILE=/etc/spire/certs/ca.crt]
    volumes:
      - ./generated/agent-gcp.conf:/etc/spire/agent.conf:ro
      - ./certs/ca.crt:/etc/spire/certs/ca.crt:ro
  spire-aws-agent:
    image: ghcr.io/spiffe/spire-agent:1.11.2
    entrypoint: ["sleep", "infinity"]
    environment: [SSL_CERT_FILE=/etc/spire/certs/ca.crt]
    volumes:
      - ./generated/agent-aws.conf:/etc/spire/agent.conf:ro
      - ./certs/ca.crt:/etc/spire/certs/ca.crt:ro
  forge-serve:
    image: alpine
    command: ["/usr/local/bin/forge", "serve"]
    ports: ["8080:8080"]
    environment:
      - SSL_CERT_FILE=/etc/spire/certs/ca.crt
      - FORGE_LOCAL_TRUST_DOMAIN=forge.aws.local
      - FORGE_REMOTE_TRUST_DOMAIN=forge.gcp.local
      - FORGE_BUNDLE_ENDPOINT_URL=https://spire-gcp-server:8443
      - FORGE_LISTEN_ADDR=:8080
    volumes:
      - ./generated/forge:/usr/local/bin/forge:ro
      - ./certs/ca.crt:/etc/spire/certs/ca.crt:ro
```

- [ ] **Step 3: Make run.sh executable**

Run: `chmod +x demo/run.sh`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add demo/run.sh demo/docker-compose.yml
git commit -m "feat(demo): container/Docker orchestration for the federation demo"
```

---

## Task 10: Makefile target + integration smoke test

**Files:**
- Modify: `Makefile`
- Create: `demo/integration_test.go`

- [ ] **Step 1: Add targets** to `Makefile` (add to `.PHONY` and append)

Change the `.PHONY` line to include the new targets:

```make
.PHONY: help build test vet lint clean preview up destroy tidy hooks demo demo-clean
```

Append:

```make
demo: ## Run the local cross-cloud federation proof (Apple container; DEMO_RUNTIME=docker for Docker)
	./demo/run.sh

demo-clean: ## Tear down demo containers, network, and generated artifacts
	-container rm -f spire-gcp-server spire-aws-server spire-gcp-agent spire-aws-agent forge-serve 2>/dev/null
	-container network rm forge-demo 2>/dev/null
	-docker compose -f demo/docker-compose.yml down 2>/dev/null
	rm -rf demo/generated demo/certs
```

- [ ] **Step 2: Write the gated integration test** `demo/integration_test.go`

```go
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
	cmd := exec.Command("bash", "run.sh")
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
```

- [ ] **Step 3: Verify the default suite ignores the demo test**

Run: `go test ./...`
Expected: PASS, and `demo` package shows `[no test files]` (the only test is behind the `demo` tag).

- [ ] **Step 4: Verify the tagged test compiles**

Run: `go vet -tags demo ./demo/`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add Makefile demo/integration_test.go
git commit -m "feat(demo): make demo target + gated federation smoke test"
```

---

## Task 11: Documentation

**Files:**
- Create: `docs/why-this-model.md`
- Modify: `README.md`, `CLAUDE.md`, `TODO.md`

- [ ] **Step 1: Write** `docs/why-this-model.md`

```markdown
# Why This Trust Model Is Valid

Forge demonstrates cross-cloud workload authentication and authorization built
on SPIFFE/SPIRE federation. This note explains *why* the model is sound — the
claim the POC exists to prove.

## The problem

A workload in GCP needs to call a service in AWS. The traditional answers are
weak: long-lived API keys (leak, rarely rotated), static cloud IAM trust
(coarse, cloud-specific), or "it's on the trusted network so allow it" (network
position is not identity).

## The model

1. **Identity, not network position.** Each workload gets a SPIFFE ID
   (`spiffe://forge.gcp.local/workload/...`) backed by a short-lived,
   cryptographically verifiable SVID issued by its cloud's SPIRE server.
2. **Federation by trust-bundle exchange (RFC 9409).** The two SPIRE servers
   exchange signing bundles, so AWS can verify an SVID minted in GCP without any
   shared secret and without either cloud's IAM trusting the other directly.
3. **Audience-scoped, short-lived tokens.** A JWT-SVID is minted for a specific
   audience (the remote trust domain) and expires quickly — no standing
   credential to steal.
4. **Authorization is separate from authentication.** Proving identity (SPIFFE)
   and deciding access (Cedar ABAC) are distinct steps. A valid SVID is
   necessary but not sufficient.
5. **Reachability is decoupled from trust.** The Bowtie mesh provides the network
   path; SPIFFE provides identity. A workload must both reach the service *and*
   prove identity *and* pass policy — defense in depth across two CSPs.

## What the POC proves

`make demo` stands up two federated SPIRE servers (GCP and AWS roles), mints a
JWT-SVID for a GCP workload audienced to the AWS trust domain, and validates it
through `forge serve` — the same code path the live deployment uses. If the same
configs federate locally, the cloud version is networking glue, not model risk.

## What is deliberately deferred (Phase 2)

Live GCP/AWS provisioning, cloud-native node attestation (`gcp_iit`/`aws_iid`),
KMS-backed upstream CA, and SPIFFE-mTLS bundle endpoints. None change the model;
they harden the transport and attestation around it.
```

- [ ] **Step 2: Add a demo section to `README.md`** (insert after the "Getting Started" section)

```markdown
## Local Federation Proof (Phase 1)

Prove the cross-cloud trust model end-to-end with no cloud spend:

```bash
make demo            # Apple `container` runtime (default)
DEMO_RUNTIME=docker make demo   # Docker fallback
make demo-clean      # tear down
```

This stands up two federated SPIRE servers (GCP and AWS roles), mints a JWT-SVID
on the GCP side, and validates it on the AWS side through `forge serve`. See
`docs/why-this-model.md` for why the model is valid and
`docs/superpowers/specs/2026-06-15-spire-bootstrap-local-proof-design.md` for the
design.
```

- [ ] **Step 3: Update `CLAUDE.md`** — add `pkg/spire/` and `demo/` to the Code Layout block:

In the Code Layout fenced block, add these two lines (after the `pkg/policies/` line):

```
pkg/spire/              → config.go: renders federation-aware SPIRE server/agent HCL (shared by VM scripts + demo)
demo/                   → local cross-cloud federation proof (gen, certs, bootstrap, run.sh, compose)
```

And under "## Commands", add:

```bash
# Run the local cross-cloud federation proof (no cloud spend)
make demo
```

- [ ] **Step 4: Update `TODO.md`** — replace the post-provision bootstrap line:

Change:

```
- [ ] Post-provision SPIRE bootstrap: federation registration, upstream CA wiring, agent join tokens
```

to:

```
- [x] Post-provision SPIRE bootstrap — Phase 1 local proof: federation bundle exchange, registration entry, agent join token, cross-cloud SVID validation (`make demo`)
- [ ] Post-provision SPIRE bootstrap — Phase 2 live: wire bootstrap against live GCP/AWS VMs, real upstream CA, cloud-native node attestors
```

- [ ] **Step 5: Build + test + commit**

Run: `go build ./... && go test ./...`
Expected: PASS.

```bash
git add docs/why-this-model.md README.md CLAUDE.md TODO.md
git commit -m "docs: document local federation proof and why the model is valid"
```

---

## Final Verification

- [ ] **Run the full unit suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS, fast (no containers pulled).

- [ ] **Run the end-to-end demo** (requires Apple `container` or Docker running)

Run: `make demo`
Expected: ends with `PASS: cross-cloud SVID validated (remote td=forge.gcp.local)`.

- [ ] **Tear down**

Run: `make demo-clean`

---

## Known Verification Points (confirm on first live run)

These are concrete spots where SPIRE 1.11.2's exact behavior should be confirmed
against the running system; each fails loudly rather than silently:

1. **`https_web` + `serving_cert_file` HCL syntax** (Tasks 1–2). If `spire-server`
   rejects the config on startup, adjust the `serverTemplate` block and
   regenerate the goldens (`go test ./pkg/spire/` will then re-pin them).
2. **`spire-server` binary path inside the image** (`/opt/spire/bin/spire-server`)
   and the agent socket path (`/tmp/agent.sock`) — confirm against the
   `ghcr.io/spiffe/spire-*:1.11.2` images and adjust `bootstrap.sh` if different.
3. **`token generate` / `api fetch jwt` stdout parsing** (Task 8) — the `sed`
   extractors assume specific line shapes; the empty-token guards will catch
   drift.
4. **Apple `container` flag parity** — `-v`, `--network`, `-e`, `--entrypoint`,
   `exec -i`. If any differ in your `container` version, adjust `run.sh`; the
   Docker fallback (`DEMO_RUNTIME=docker`) is the reference behavior.
```
