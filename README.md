# forge

[![CI](https://github.com/wingnut128/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/ci.yml)
[![CodeQL](https://github.com/wingnut128/forge/actions/workflows/codeql.yml/badge.svg)](https://github.com/wingnut128/forge/actions/workflows/codeql.yml)
[![Semgrep](https://img.shields.io/badge/semgrep-scanning-blue)](https://semgrep.dev/orgs/mlapane_github_personal_org/projects/5345292)
[![Go Report Card](https://goreportcard.com/badge/github.com/wingnut128/forge)](https://goreportcard.com/report/github.com/wingnut128/forge)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/wingnut128/forge/badge)](https://scorecard.dev/viewer/?uri=github.com/wingnut128/forge)

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
│   │   ├── gcp/            # GCP infrastructure (VPC, GKE, Workload Identity)
│   │   └── aws/            # AWS infrastructure (VPC, EKS, SPIRE OIDC)
│   ├── attestation/        # Cross-cloud trust bundle & SPIFFE federation
│   └── config/             # Stack configuration helpers
└── stacks/                 # Per-environment stack configs
```

## Current Scope

- **GCP foundation**: VPC, GKE, Workload Identity Federation
- **AWS foundation**: VPC, EKS, SPIRE OIDC Provider
- **SPIFFE trust domain federation** (GCP <-> AWS) with bundle refresh (RFC 9409)
- **Cross-cloud SVID token validation** via `forge serve` HTTP API
- **Cedar-based ABAC authorization** for workload access control
- **Policy-as-code**: Go-based infrastructure policy checks (mandatory/advisory)

## Planned

- Temporal worker integration for durable deployment orchestration
- Tiered approval gates (autonomous / async / synchronous)
- Pluggable Cedar policy storage (S3, GCS, PostgreSQL, stdin)
- Cloud landing zones (optional GCP project/AWS account provisioning)
- AWS Secrets Manager / KMS stack for cert provisioning

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
