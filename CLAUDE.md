# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Forge is a cross-cloud workload attestation platform built with Pulumi (Go) and SPIFFE/SPIRE. It provisions GCP infrastructure (VPC, GKE, Workload Identity Federation) and configures cross-cloud SPIFFE trust domain federation between GCP and AWS.

## Commands

```bash
# Install/sync dependencies
go mod tidy

# Preview infrastructure changes (dry-run)
FORGE_STACK=dev go run ./cmd/forge preview

# Deploy infrastructure
FORGE_STACK=dev go run ./cmd/forge up

# Tear down infrastructure
FORGE_STACK=dev go run ./cmd/forge destroy

# Build
go build ./...

# Run tests
go test ./...

# Run a single test
go test ./pkg/... -run TestName
```

`FORGE_STACK` defaults to `"dev"` if unset.

## Prerequisites

- Go 1.25+
- Pulumi CLI installed
- GCP credentials via `gcloud auth application-default login`
- Pulumi stack initialized (`pulumi stack init dev`)

## Required Pulumi Stack Config

Set via `pulumi config set forge:<key> <value>`:

| Key | Required | Default |
|-----|----------|---------|
| `environment` | yes | — |
| `spire-trust-domain` | yes | — |
| `aws-spire-trust-domain` | yes | — |
| `gke-node-count` | no | 3 |
| `gke-machine-type` | no | e2-standard-4 |

## Architecture

The entrypoint (`main.go`) uses Pulumi's **Automation API** (`auto.UpsertStackInlineSource`) to run an inline Pulumi program — no separate `Pulumi.yaml` needed. The CLI accepts `preview`, `up`, or `destroy`.

### Code Layout

```
cmd/forge/              → main.go: Automation API entrypoint, wires the deploy pipeline
pkg/config/             → config.go: loads ForgeConfig from Pulumi stack config
pkg/components/gcp/     → network.go, gke.go, workload_identity.go (Pulumi component resources)
pkg/attestation/        → trust.go: SPIFFE trust domain and federation pair structs (WIP)
```

### Component Resource Pattern

All infrastructure components follow Pulumi's component resource pattern:
1. `*Args` struct for configuration inputs
2. Struct embedding `pulumi.ResourceState` with exported `pulumi.Output` fields
3. `New*()` constructor that calls `RegisterComponentResource` and `RegisterResourceOutputs`
4. Resources created as children via `pulumi.Parent(component)`

Resource naming convention: `forge-{environment}-{resource}` (e.g., `forge-dev-vpc`).

Component type URNs: `forge:gcp:Network`, `forge:gcp:GKECluster`, `forge:gcp:WorkloadIdentity`.

### Deploy Pipeline Flow

`deployFunc` in `main.go` chains: config → Network → GKECluster (depends on network outputs) → WorkloadIdentity (depends on cluster name).

## Key Design Decisions

- **Private GKE cluster**: nodes have no public IPs; control plane is accessible externally (for kubectl)
- **Workload Identity Federation** accepts OIDC JWTs from AWS SPIRE's OIDC Discovery Provider, enabling cross-cloud SVID validation without shared secrets
- **Region**: hardcoded to `us-central1`
- **GKE release channel**: REGULAR
