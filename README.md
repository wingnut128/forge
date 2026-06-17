# forge

[![CI](https://github.com/wingnut128/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/ci.yml)
[![CodeQL](https://github.com/wingnut128/forge/actions/workflows/codeql.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/wingnut128/forge/branch/main/graph/badge.svg)](https://codecov.io/gh/wingnut128/forge)
[![Go Report Card](https://goreportcard.com/badge/github.com/wingnut128/forge)](https://goreportcard.com/report/github.com/wingnut128/forge)

Cross-cloud workload attestation platform built with Pulumi (Go) and SPIFFE/SPIRE.

## Overview

Forge provisions and manages cross-cloud infrastructure with a focus on workload identity
and attestation. Built on Pulumi's Automation API for programmatic infrastructure management,
designed to integrate with agentic deployment pipelines.

## Architecture

```
forge/
├── cmd/forge/              # Automation API entrypoint
├── pkg/
│   ├── components/         # Reusable Pulumi component resources
│   │   ├── gcp/            # GCP infrastructure (VPC, GKE, WIF, AR, Cloud Run, Cloud Build)
│   │   └── aws/            # AWS infrastructure (VPC, EKS, SPIRE OIDC)
│   ├── attestation/        # Cross-cloud trust bundle & SPIFFE federation
│   └── config/             # Stack configuration helpers
└── stacks/                 # Per-environment stack configs
```

## Current Scope

- **GCP foundation**: VPC (multi-subnet, Cloud NAT, mgmt subnet), optional GKE, Workload Identity Federation, Artifact Registry, Cloud Run, Cloud Build triggers
- **AWS foundation**: VPC (private + public subnets, IGW, NAT Gateway), optional EKS, SPIRE OIDC Provider
- **SPIRE testing track (default)**: one cheap VM per CSP (GCE + EC2) with persistent state on disk, daily snapshot schedules
- **Bowtie controllers**: one Bowtie VM per CSP for cross-cloud mesh + admin access (license/admin bootstrap is out-of-band)
- **Optional managed-state track**: Cloud SQL + RDS Postgres for SPIRE DataStore, KMS CMKs for `gcp_kms` / `aws_kms` KeyManager, Secret Manager / Secrets Manager for admin tokens
- **SPIFFE trust domain federation** (GCP <-> AWS) with bundle refresh (RFC 9409)
- **Cross-cloud SVID token validation** via `forge serve` HTTP API
- **Cedar-based ABAC authorization** for workload access control
- **Policy-as-code**: Go-based infrastructure policy checks (mandatory/advisory)

## Test Tracks and Cost

| Flags | What you get | Rough monthly floor |
|---|---|---|
| (defaults) | 2 VPCs + NAT + 2 SPIRE VMs (e2-small / t3.small) + 2 Bowtie VMs | ~$35-50 |
| `enable-managed-state=true` | Above + Cloud SQL db-f1-micro + RDS db.t4g.micro + KMS keys | ~$75-110 |
| `enable-gke=true,enable-eks=true` | Above + full GKE and EKS control planes/node groups | ~$250+ |
| `enable-multi-az-nat=true` | One NAT Gateway per AZ instead of a single shared one | adds ~$35 |

## Planned

- Temporal worker integration for durable deployment orchestration
- Tiered approval gates (autonomous / async / synchronous)
- Pluggable Cedar policy storage (S3, GCS, PostgreSQL, stdin)
- Cloud landing zones (optional GCP project/AWS account provisioning)
- Post-provision SPIRE federation bootstrap (registration entries, upstream CA)

## Prerequisites

- Go 1.25+
- Pulumi CLI
- GCP credentials (`gcloud auth application-default login`)
- GCP project with required APIs enabled

## Getting Started

```bash
# Install dependencies
go mod tidy

# Set up a dev stack
pulumi stack init dev

# Preview changes
go run ./cmd/forge preview

# Deploy
go run ./cmd/forge up
```

## Local Federation Proof (Phase 1)

Prove the cross-cloud trust model end-to-end with no cloud spend. Requires a
container runtime — Apple `container` (default, macOS) or Docker. The first run
pulls the SPIRE 1.11.2 images (allow a few minutes); no cloud credentials.

```bash
make demo            # Apple `container` runtime (default)
DEMO_RUNTIME=docker make demo   # Docker fallback
make demo-clean      # tear down
```

This stands up two federated SPIRE servers (GCP and AWS roles), mints a JWT-SVID
on the GCP side, and validates it on the AWS side through `forge serve` — the
real `pkg/attestation` path. A successful run ends with:

```
==> validating the SVID through forge serve (AWS role)
validate response: {"valid":true,"spiffe_id":"spiffe://forge.gcp.local/workload/demo","trust_domain":"forge.gcp.local",...}
PASS: cross-cloud SVID validated (remote td=forge.gcp.local)
```

That `PASS` is the whole thesis: a workload identity minted in one cloud,
cryptographically verified in the other via SPIFFE federation — no shared secret,
no cross-cloud IAM trust. `make demo` exits `0` on success.

See [`demo/README.md`](demo/README.md) for the architecture, the bootstrap flow,
and full expected output; [`docs/why-this-model.md`](docs/why-this-model.md) for
why the model is valid; [`docs/threat-model.md`](docs/threat-model.md) for the
STRIDE threat model and Phase-2 security gates; and the
[design spec](docs/superpowers/specs/2026-06-15-spire-bootstrap-local-proof-design.md).
