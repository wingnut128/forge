# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- **Always update documentation before committing new features.** When adding or changing functionality, update README.md, CLAUDE.md, and TODO.md as part of the same PR — not as a follow-up. Documentation includes: code layout, commands, config keys, architecture descriptions, and planned/completed scope.
- Follow the bisect commits rule from the global CLAUDE.md — each commit is a single logical change.
- Run `go build ./...`, `go test ./...`, and `go vet ./...` before committing.

## What This Is

Forge is a cross-cloud workload attestation platform built with Pulumi (Go) and SPIFFE/SPIRE. It provisions GCP and AWS infrastructure and configures bidirectional SPIFFE trust domain federation between them.

## Commands

```bash
# Install/sync dependencies
go mod tidy

# Preview infrastructure changes (dry-run)
FORGE_STACK=dev go run ./cmd/forge preview

# Deploy infrastructure (GCP + AWS)
FORGE_STACK=dev go run ./cmd/forge up

# Tear down infrastructure
FORGE_STACK=dev go run ./cmd/forge destroy

# Start attestation + authorization server
FORGE_LOCAL_TRUST_DOMAIN=forge.dev.gcp.example.com \
FORGE_REMOTE_TRUST_DOMAIN=forge.dev.aws.example.com \
FORGE_BUNDLE_ENDPOINT_URL=https://bundle.example.com \
FORGE_POLICY_DIR=./policies/examples \
go run ./cmd/forge serve

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
- AWS credentials configured
- Pulumi stack initialized (`pulumi stack init dev`)

## Required Pulumi Stack Config

Set via `pulumi config set forge:<key> <value>`:

| Key | Required | Default |
|-----|----------|---------|
| `environment` | yes | — |
| `spire-trust-domain` | yes | — |
| `aws-spire-trust-domain` | yes | — |
| `gcp-region` | no | us-central1 |
| `aws-region` | no | us-east-1 |
| `gke-node-count` | no | 3 |
| `gke-machine-type` | no | e2-standard-4 |
| `eks-node-count` | no | 3 |
| `eks-instance-type` | no | t3.medium |

## Environment Variables (forge serve)

| Variable | Required | Default |
|----------|----------|---------|
| `FORGE_LOCAL_TRUST_DOMAIN` | yes | — |
| `FORGE_REMOTE_TRUST_DOMAIN` | yes | — |
| `FORGE_BUNDLE_ENDPOINT_URL` | yes | — |
| `FORGE_LISTEN_ADDR` | no | `:8080` |
| `FORGE_POLICY_DIR` | no | (authz disabled) |

## Architecture

The entrypoint (`main.go`) uses Pulumi's **Automation API** (`auto.UpsertStackInlineSource`) to run an inline Pulumi program — no separate `Pulumi.yaml` needed. The CLI accepts `preview`, `up`, `destroy`, or `serve`.

### Code Layout

```
cmd/forge/              → main.go: Automation API entrypoint + serve command; test.go: test stack (AR, Cloud Run, Cloud Build)
pkg/config/             → config.go: loads and validates ForgeConfig from Pulumi stack config
pkg/components/gcp/     → network.go, gke.go, workload_identity.go, cloudbuild.go, cloudrun.go, artifact_registry.go (GCP Pulumi components)
pkg/components/aws/     → vpc.go, eks.go, spire_oidc.go (AWS Pulumi components)
pkg/attestation/        → trust.go, bundle.go, validate.go (SPIFFE federation + JWT-SVID validation)
pkg/orchestration/      → server.go: HTTP server for /validate and /healthz endpoints
pkg/authz/              → authz.go: Cedar-based ABAC authorization
pkg/policies/           → policy.go, gcp.go, aws.go: infrastructure policy checks
policies/examples/      → Example Cedar policies for cross-cloud access control
```

### Component Resource Pattern

All infrastructure components (GCP and AWS) follow Pulumi's component resource pattern:
1. `*Args` struct for configuration inputs
2. Struct embedding `pulumi.ResourceState` with exported `pulumi.Output` fields
3. `New*()` constructor that calls `RegisterComponentResource` and `RegisterResourceOutputs`
4. Resources created as children via `pulumi.Parent(component)`

Resource naming convention: `forge-{environment}-{resource}` (e.g., `forge-dev-vpc`).

GCP URNs: `forge:gcp:Network`, `forge:gcp:GKECluster`, `forge:gcp:WorkloadIdentity`, `forge:gcp:ArtifactRegistry`, `forge:gcp:CloudRunService`, `forge:gcp:CloudBuildTrigger`.
AWS URNs: `forge:aws:VPC`, `forge:aws:EKSCluster`, `forge:aws:SPIREOIDCProvider`.

### Deploy Pipeline Flow

`deployFunc` in `main.go` runs policy checks first, then chains:
- **GCP**: Network → GKECluster → WorkloadIdentity (accepts AWS SVIDs)
- **AWS**: VPC → EKSCluster → SPIREOIDCProvider (accepts GCP SVIDs)

### Authorization Model

Cedar-based ABAC. SPIFFE IDs map to `SpiffeWorkload` Cedar principals. The `/validate` endpoint optionally evaluates Cedar policies when `action` and `resource` fields are provided. Cedar `.cedar` files are loaded from `FORGE_POLICY_DIR`.

## Key Design Decisions

- **Private clusters**: GKE nodes have no public IPs; EKS uses private endpoint. Control planes accessible externally.
- **Workload Identity Federation** (GCP) accepts OIDC JWTs from AWS SPIRE; **IAM OIDC Provider** (AWS) accepts JWTs from GCP SPIRE. Bidirectional federation.
- **Regions**: Configurable via `gcp-region` / `aws-region` stack config (defaults: `us-central1`, `us-east-1`)
- **CIDRs**: GCP `10.0.0.0/20`, AWS `10.1.0.0/16` (non-overlapping)
- **Policy checks run before provisioning** — mandatory violations block deployment
- **Authorization is opt-in** at both server level (no `FORGE_POLICY_DIR` = disabled) and request level (omit action/resource fields)
