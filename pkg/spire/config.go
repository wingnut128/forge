// Package spire renders federation-aware SPIRE server and agent configuration.
// Pure functions, no I/O — both the VM startup scripts and the local demo
// harness render config from here so they can never drift.
package spire

import (
	"fmt"
	"strconv"
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
	// BundleEndpointSyncInterval is the serving_cert_file file_sync_interval.
	// SPIRE 1.11.x requires this duration (empty -> "invalid duration" error).
	BundleEndpointSyncInterval string // default "1h"

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
                    file_sync_interval = "{{.BundleEndpointSyncInterval}}"
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
		return "", fmt.Errorf("TrustDomain is required")
	}
	if cfg.PeerTrustDomain == "" {
		return "", fmt.Errorf("PeerTrustDomain is required")
	}
	if cfg.PeerBundleEndpointURL == "" {
		return "", fmt.Errorf("PeerBundleEndpointURL is required")
	}
	applyServerDefaults(&cfg)

	port, portErr := strconv.Atoi(cfg.BundleEndpointPort)
	if portErr != nil {
		return "", fmt.Errorf("BundleEndpointPort %q is not a valid port: %w", cfg.BundleEndpointPort, portErr)
	}
	if port <= 0 {
		return "", fmt.Errorf("BundleEndpointPort %q is not a valid port: must be positive", cfg.BundleEndpointPort)
	}

	data := serverTemplateData{ServerConfig: cfg}
	switch cfg.StateMode {
	case StateModeDisk:
		data.DBType = "sqlite3"
		data.ConnString = cfg.DataDir + "/datastore.sqlite3"
	case StateModeManaged:
		if cfg.ManagedDBConnString == "" {
			return "", fmt.Errorf("ManagedDBConnString is required for managed mode")
		}
		data.DBType = "postgres"
		data.ConnString = cfg.ManagedDBConnString
	default:
		return "", fmt.Errorf("unknown StateMode %q", cfg.StateMode)
	}

	var sb strings.Builder
	if err := serverTemplate.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("rendering server config: %w", err)
	}
	return sb.String(), nil
}

// AgentConfig is the input for RenderAgentHCL.
type AgentConfig struct {
	TrustDomain   string // required
	ServerAddress string // required, e.g. "spire-gcp-server"
	ServerPort    string // default "8081"
	DataDir       string // default "/var/lib/spire/agent"
	LogLevel      string // default "INFO"
	SocketPath    string // default "/tmp/agent.sock" (Workload API socket)
	// InsecureBootstrap lets the agent accept the server's trust bundle on first
	// connect without a pre-shared trust_bundle_path. True is fine for the local
	// demo; Phase 2 (live) should pin a trust bundle instead. Defaults false.
	InsecureBootstrap bool
}

var agentTemplate = template.Must(template.New("agent").Parse(
	`agent {
    data_dir = "{{.DataDir}}"
    log_level = "{{.LogLevel}}"
    trust_domain = "{{.TrustDomain}}"
    server_address = "{{.ServerAddress}}"
    server_port = "{{.ServerPort}}"
    socket_path = "{{.SocketPath}}"
    insecure_bootstrap = {{.InsecureBootstrap}}
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
		return "", fmt.Errorf("TrustDomain is required")
	}
	if cfg.ServerAddress == "" {
		return "", fmt.Errorf("ServerAddress is required")
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
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/tmp/agent.sock"
	}

	var sb strings.Builder
	if err := agentTemplate.Execute(&sb, cfg); err != nil {
		return "", fmt.Errorf("rendering agent config: %w", err)
	}
	return sb.String(), nil
}

// RenderServerStartupScript returns a bash script that formats and mounts a
// data disk, downloads SPIRE, writes the server config, installs a systemd
// unit, and starts the service. devicePath is the OS device path for the data
// volume (e.g. "/dev/disk/by-id/google-spire-data" on GCP, "/dev/xvdf" on AWS).
func RenderServerStartupScript(version, serverHCL, devicePath string) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
SPIRE_VERSION=%q

DEV=%s
mkdir -p /var/lib/spire
if ! mountpoint -q /var/lib/spire; then
  if ! blkid "$DEV" >/dev/null 2>&1; then
    mkfs.ext4 -F "$DEV"
  fi
  echo "$DEV /var/lib/spire ext4 defaults,nofail 0 2" >> /etc/fstab
  mount /var/lib/spire
fi

if [ ! -x /usr/local/bin/spire-server ]; then
  cd /tmp
  SPIRE_PKG="spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz"
  SPIRE_BASE="https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}"
  # Retry generously: this VM may boot before the NAT instance finishes
  # bringing up iptables (~20-30s). curl's default backoff gives up in ~7s,
  # and with 'set -euo pipefail' a failed download aborts the whole script.
  # --retry-all-errors is required because connection refused/timeout is not
  # one of curl's default "transient" retry conditions.
  curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
    --retry 10 --retry-delay 5 --retry-all-errors \
    -o "${SPIRE_PKG}" "${SPIRE_BASE}/${SPIRE_PKG}"
  curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
    --retry 10 --retry-delay 5 --retry-all-errors \
    -o "${SPIRE_PKG%%.tar.gz}_sha256sum.txt" "${SPIRE_BASE}/${SPIRE_PKG%%.tar.gz}_sha256sum.txt"
  # Fail closed if the published checksum does not match the downloaded archive.
  sha256sum -c "${SPIRE_PKG%%.tar.gz}_sha256sum.txt"
  tar -xzf "${SPIRE_PKG}"
  install -m 0755 "spire-${SPIRE_VERSION}/bin/spire-server" /usr/local/bin/spire-server
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
`, version, devicePath, serverHCL)
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
	if cfg.BundleEndpointSyncInterval == "" {
		cfg.BundleEndpointSyncInterval = "1h"
	}
}
