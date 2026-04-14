# Forge TODO

## High Priority (blocking federation)
- [x] Implement SPIFFE Bundle Endpoint refresh loop in `pkg/attestation` (RFC 9409)
- [x] Add SVID validation runtime code
- [x] Export fields in `TrustDomain`/`FederationPair` for serialization

## Medium Priority (tests & reliability)
- [x] Add tests for `pkg/config` (defaults, validation, edge cases)
- [x] Add tests for `pkg/components/gcp` (Pulumi component tests)
- [x] Add input validation in `pkg/config` (trust domain format, environment allowlist)

## Medium Priority (authorization)
- [ ] Pluggable Cedar policy storage — support loading policies from S3, GCS, PostgreSQL, stdin (e.g., piped from Consul KV), not just local directory

## Medium Priority (infrastructure)
- [ ] Cloud landing zones — optional provisioning of GCP project/org and AWS account/VPC foundation (conditional flag, not always needed)
- [x] Well-architected VPCs (Cloud NAT / NAT Gateway, multi-AZ, mgmt subnet)
- [x] Cheap VM-based SPIRE server track (GCE + EC2, disk-backed state)
- [x] Bowtie controller infrastructure (one VM per CSP, admin firewall)
- [x] Feature-flagged managed state (Cloud SQL + RDS + KMS + Secret Manager)
- [ ] Post-provision SPIRE bootstrap: federation registration, upstream CA wiring, agent join tokens
- [ ] Bowtie licensing + initial admin bootstrap automation

## Low Priority (features & infra)
- [x] Implement AWS components (`pkg/components/aws/`)
- [ ] Add Temporal worker integration
- [x] Add policy-as-code via Pulumi CrossGuard
- [x] Add agent orchestration layer
- [ ] Create example stack config (`Pulumi.dev.yaml.example`)
