# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- **Always update documentation before committing new features.** When adding or changing functionality, update README.md, CLAUDE.md, and TODO.md as part of the same PR — not as a follow-up. Documentation includes: code layout, commands, config keys, architecture descriptions, and planned/completed scope.
- Follow the bisect commits rule from the global CLAUDE.md — each commit is a single logical change.
- Install git pre-commit hooks once per clone: `make hooks`. The hook runs `make lint` (gofmt check, `go vet`, `golangci-lint`, `go build`) before every commit — the same gate as the CI Lint job. Linting is mandatory; commits and PRs must pass it.
- For GitHub operations, use the `gh` CLI. CI runs on GitHub Actions (`.github/workflows/`).

## CI and Dependency Automation

Workflows in `.github/workflows/`:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | push / PR | Build & Test, Coverage, Lint |
| `codeql.yml` | push / PR / schedule | CodeQL security analysis |
| `dependabot-auto-merge.yml` | `pull_request` | Auto-merges Dependabot patch/minor bumps |

`main` requires the **Build & Test** and **Lint** checks to pass. Code-owner review is *not* required — `.github/CODEOWNERS` routes review requests but does not gate merges.

### Dependabot auto-merge

`dependabot-auto-merge.yml` enables GitHub auto-merge (squash) on Dependabot PRs when the update is **semver-patch or semver-minor**. Major bumps are deliberately left for manual review. Required CI checks still gate the merge, so a failing bump never lands.

It uses `GITHUB_TOKEN` only — there is no PAT and no credential to rotate. Do not reintroduce one: a long-lived token here previously expired unnoticed, because on major bumps the merge step is skipped by the semver condition and the job reports green without ever exercising the token.

### Action version pinning

Actions are pinned to **floating major tags** (`@v4`, `@v7`) so patches and minors are picked up automatically. `github/codeql-action` is excluded from patch/minor Dependabot updates in `.github/dependabot.yml` — left alone, Dependabot proposes narrowing `@v4` to a fixed patch pin, which lands stale and generates a PR per patch. Major bumps still open PRs for every action.

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

# Run the local cross-cloud federation proof (no cloud spend)
make demo
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
| `spire-aws-ami` | yes | — (Amazon Linux 2023 AMI for your region) |
| `gcp-region` | no | us-central1 |
| `aws-region` | no | us-east-1 |
| `enable-gke` | no | false (opt in to GKE control plane + node pool) |
| `enable-eks` | no | false (opt in to EKS control plane + node group) |
| `enable-managed-state` | no | false (opt in to Cloud SQL + RDS + KMS + Secret Manager) |
| `enable-multi-az-nat` | no | false (when false, a single NAT instance serves both AZs) |
| `enable-bowtie` | no | false (opt in to the Bowtie controller VM per cloud) |
| `gke-node-count` | no | 3 |
| `gke-machine-type` | no | e2-standard-4 |
| `eks-node-count` | no | 3 |
| `eks-instance-type` | no | t3.medium |
| `bowtie-gcp-image` | conditional | — (image self-link, e.g. `projects/bowtie-works/global/images/bowtie-controller-gce-efi-<version>`; required when `enable-bowtie=true`) |
| `bowtie-aws-ami` | conditional | — (Bowtie controller AMI, owner account `055761336000`; required when `enable-bowtie=true`) |
| `bowtie-admin-cidrs` | no | [] (list of CIDRs allowed to reach Bowtie admin ports; empty = locked to 127.0.0.1/32) |
| `spire-server-version` | no | 1.11.2 |
| `spire-db-password` | conditional | — (pulumi config set --secret; required when `enable-managed-state=true`) |

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
cmd/forge/              → main.go: thin CLI entrypoint (os.Args dispatch + Automation API calls)
                          deploy.go: deployFunc + provisioning phase helpers
                          serve.go: runServe (attestation + authz HTTP server)
pkg/config/             → config.go: loads and validates ForgeConfig from Pulumi stack config
pkg/components/gcp/     → network.go, gke.go, workload_identity.go,
                          spire_server.go, bowtie.go, managed_state.go (GCP Pulumi components)
pkg/components/aws/     → vpc.go, fcknat.go, eks.go, spire_oidc.go, spire_server.go, bowtie.go, managed_state.go (AWS Pulumi components)
pkg/attestation/        → trust.go, bundle.go, validate.go (SPIFFE federation + JWT-SVID validation)
pkg/orchestration/      → server.go: HTTP server for /validate and /healthz endpoints
pkg/authz/              → authz.go: Cedar-based ABAC authorization
pkg/policies/           → policy.go, gcp.go, aws.go: infrastructure policy checks
pkg/spire/              → config.go: renders federation-aware SPIRE server/agent HCL (shared by VM scripts + demo)
demo/                   → local cross-cloud federation proof (gen, certs, bootstrap, run.sh, compose)
policies/examples/      → Example Cedar policies for cross-cloud access control
```

### Component Resource Pattern

All infrastructure components (GCP and AWS) follow Pulumi's component resource pattern:
1. `*Args` struct for configuration inputs
2. Struct embedding `pulumi.ResourceState` with exported `pulumi.Output` fields
3. `New*()` constructor that calls `RegisterComponentResource` and `RegisterResourceOutputs`
4. Resources created as children via `pulumi.Parent(component)`

Resource naming convention: `forge-{environment}-{resource}` (e.g., `forge-dev-vpc`).

GCP URNs: `forge:gcp:Network`, `forge:gcp:GKECluster`, `forge:gcp:WorkloadIdentity`, `forge:gcp:SPIREServer`, `forge:gcp:BowtieController`, `forge:gcp:ManagedState`.
AWS URNs: `forge:aws:VPC`, `forge:aws:EKSCluster`, `forge:aws:SPIREOIDCProvider`, `forge:aws:SPIREServer`, `forge:aws:BowtieController`, `forge:aws:ManagedState`.

### Deploy Pipeline Flow

`deployFunc` in `main.go` runs policy checks first, then chains:
- **GCP foundation**: Network (VPC + mgmt subnet + Cloud NAT) → optionally GKECluster + WorkloadIdentity → optionally ManagedState → SPIREServer VM → BowtieController VM
- **AWS foundation**: VPC (private/public subnets + IGW + fck-nat) → optionally EKSCluster + SPIREOIDCProvider → optionally ManagedState → SPIREServer EC2 → BowtieController EC2

### AWS egress: fck-nat, not NAT Gateway

Private-subnet egress runs through [fck-nat](https://fck-nat.dev) NAT instances (`pkg/components/aws/fcknat.go`) instead of a managed NAT Gateway — roughly $10/month against $36.50, and the SPIRE server's only real egress need is a one-time ~30 MB download at boot.

Each fleet is an ASG pinned to `min=max=desired=1`: a self-healing single instance, not a scaled fleet. Routing targets a **persistent ENI**, not the instance, so a replacement instance re-attaches the same ENI and the route tables never change — no Lambda, no route rewriting.

Two things that will silently break it if changed:
- `SourceDestCheck` must stay `false` on the ENI, or the kernel drops every forwarded packet with no error and no log.
- The NAT security group must only admit the VPC CIDR. A `0.0.0.0/0` ingress rule turns it into an open relay.

The AMI is discovered via `LookupAmi` against owner `568608671756`, name `fck-nat-al2023-*`, architecture `arm64`. Override with `FckNatAMIOwner` / `FckNatAMINamePattern` / `FckNatAMIArchitecture` on `VPCArgs` if the vendor rotates any of them.

Both the NAT instances and the SPIRE server EC2 carry an instance profile with `AmazonSSMManagedInstanceCore`. Neither has a key pair and both sit behind private routing, so SSM Session Manager is the only way onto either box — without it, a failed boot is undiagnosable. The SPIRE server's download also retries for ~50s (`--retry 10 --retry-delay 5 --retry-all-errors`) because it can boot before the NAT instance has finished bringing up iptables.

**The architecture filter is load-bearing.** The name pattern matches both architectures, and fck-nat publishes the x86_64 image a few minutes ahead of arm64 — so `MostRecent` without an architecture filter selects x86_64 and hands it to a `t4g.nano`, which fails to boot. If you change `NATInstanceType` to an x86 type, change `FckNatAMIArchitecture` to `x86_64` in the same edit.

The default flags (`enable-gke=false`, `enable-eks=false`, `enable-managed-state=false`, `enable-bowtie=false`) produce the cheap VM-based SPIRE test track. Flip flags to opt into the K8s, managed-state, or Bowtie paths.

`enable-bowtie` gates both controller VMs. The Bowtie mesh is orthogonal to the SPIFFE trust claim the POC proves, so it stays off by default — enabling it requires `bowtie-gcp-image` and `bowtie-aws-ami`. The VMs are sized to the documented vendor minimum and no higher (2 cores / 4 GB RAM / 50 GB disk → `e2-medium` + `t3.medium`), which costs roughly $60/month across both clouds.

### Authorization Model

Cedar-based ABAC. SPIFFE IDs map to `SpiffeWorkload` Cedar principals. The `/validate` endpoint optionally evaluates Cedar policies when `action` and `resource` fields are provided. Cedar `.cedar` files are loaded from `FORGE_POLICY_DIR`.

## Key Design Decisions

- **Private clusters**: GKE nodes have no public IPs; EKS uses private endpoint. Control planes accessible externally.
- **Workload Identity Federation** (GCP) accepts OIDC JWTs from AWS SPIRE; **IAM OIDC Provider** (AWS) accepts JWTs from GCP SPIRE. Bidirectional federation.
- **Regions**: Configurable via `gcp-region` / `aws-region` stack config (defaults: `us-central1`, `us-east-1`)
- **CIDRs**: GCP `10.0.0.0/20`, AWS `10.1.0.0/16` (non-overlapping)
- **Policy checks run before provisioning** — mandatory violations block deployment
- **Authorization is opt-in** at both server level (no `FORGE_POLICY_DIR` = disabled) and request level (omit action/resource fields)
