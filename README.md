# forge

[![CI](https://github.com/wingnut128/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/ci.yml)
[![CodeQL](https://github.com/wingnut128/forge/actions/workflows/codeql.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/codeql.yml)
[![Semgrep](https://github.com/wingnut128/forge/actions/workflows/semgrep.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/semgrep.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/wingnut128/forge)](https://goreportcard.com/report/github.com/wingnut128/forge)
[![codecov](https://codecov.io/gh/wingnut128/forge/branch/main/graph/badge.svg)](https://codecov.io/gh/wingnut128/forge)

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
