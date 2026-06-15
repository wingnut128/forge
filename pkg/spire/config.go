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

	port, portErr := strconv.Atoi(cfg.BundleEndpointPort)
	if portErr != nil {
		return "", fmt.Errorf("spire: BundleEndpointPort %q is not a valid port: %w", cfg.BundleEndpointPort, portErr)
	}
	if port <= 0 {
		return "", fmt.Errorf("spire: BundleEndpointPort %q is not a valid port: must be positive", cfg.BundleEndpointPort)
	}

	data := serverTemplateData{ServerConfig: cfg}
	switch cfg.StateMode {
	case StateModeDisk:
		data.DBType = "sqlite3"
		data.ConnString = cfg.DataDir + "/datastore.sqlite3"
	case StateModeManaged:
		if cfg.ManagedDBConnString == "" {
			return "", fmt.Errorf("spire: ManagedDBConnString is required for managed mode")
		}
		data.DBType = "postgres"
		data.ConnString = cfg.ManagedDBConnString
	default:
		return "", fmt.Errorf("spire: unknown StateMode %q", cfg.StateMode)
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
