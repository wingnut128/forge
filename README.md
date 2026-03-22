# forge

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
│   │   └── aws/            # AWS infrastructure (future)
│   ├── attestation/        # Cross-cloud trust bundle & SPIFFE federation
│   └── config/             # Stack configuration helpers
└── stacks/                 # Per-environment stack configs
```

## Current Scope

- GCP foundation: VPC, GKE, Workload Identity Federation
- SPIFFE trust domain federation (GCP <-> AWS)
- Cross-cloud SVID token validation

## Planned

- Temporal worker integration for durable deployment orchestration
- Tiered approval gates (autonomous / async / synchronous)
- Policy-as-code via Pulumi CrossGuard
- AWS infrastructure components
- Agent orchestration layer

## Prerequisites

- Go 1.22+
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
