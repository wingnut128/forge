# Forge TODO

## High Priority (blocking federation)
- [x] Implement SPIFFE Bundle Endpoint refresh loop in `pkg/attestation` (RFC 9409)
- [x] Add SVID validation runtime code
- [x] Export fields in `TrustDomain`/`FederationPair` for serialization

## Medium Priority (tests & reliability)
- [x] Add tests for `pkg/config` (defaults, validation, edge cases)
- [x] Add tests for `pkg/components/gcp` and `pkg/components/aws` (Pulumi component tests, all six components per cloud: network/VPC, K8s cluster, federation glue, SPIRE server, Bowtie controller, managed state)
- [x] Add tests for `cmd/forge` deploy pipeline config-validation branches (missing `spire-aws-ami`/`bowtie-gcp-image`/`bowtie-aws-ami`) and `demo/gen`
- [x] Add input validation in `pkg/config` (trust domain format, environment allowlist)
- [ ] Refactor `runServe` to accept an injectable `context.Context` so the Cedar-authz-enabled code path can be covered by tests (currently blocked on `signal.NotifyContext` binding to real OS signals)

## Medium Priority (authorization)
- [ ] Pluggable Cedar policy storage — support loading policies from S3, GCS, PostgreSQL, stdin (e.g., piped from Consul KV), not just local directory

## Medium Priority (infrastructure)
- [ ] Cloud landing zones — optional provisioning of GCP project/org and AWS account/VPC foundation (conditional flag, not always needed)
- [x] Well-architected VPCs (Cloud NAT / NAT Gateway, multi-AZ, mgmt subnet)
- [x] Replace the AWS NAT Gateway with a self-healing fck-nat ASG (min=max=1) behind a persistent ENI — ~$36.50/mo → ~$10/mo
- [x] Verify the fck-nat AMI resolves live (owner `568608671756`, `fck-nat-al2023-*`, arm64 — confirmed against the live EC2 API)
- [x] Cheap VM-based SPIRE server track (GCE + EC2, disk-backed state)
- [x] Bowtie controller infrastructure (one VM per CSP, admin firewall)
- [x] Feature-flag the Bowtie controllers behind `enable-bowtie` (default false) — they sit outside the SPIFFE trust claim and cost ~$37/mo
- [x] Size the Bowtie VMs to the documented vendor minimum (2 cores / 4 GB / 50 GB): `e2-medium` + `t3.medium`, 50 GB disks
- [x] Feature-flagged managed state (Cloud SQL + RDS + KMS + Secret Manager)
- [x] Post-provision SPIRE bootstrap — **Phase 1 local proof complete** (verified live via `make demo` on Apple `container`; MR !51): federation bundle exchange, registration entry, agent join token, GCP-minted JWT-SVID validated on the AWS-role `forge serve`
- [x] Cross-cloud private transport — WireGuard point-to-point tunnel (`enable-vpn`), GCE gateway VM ↔ AWS fck-nat instance, so SPIRE is reachable only over the VPN
- [x] Repoint `PeerBundleEndpointURL` at the peer's pinned private IP, routed over the tunnel (was `https://<peer-trust-domain>:8443`, which resolved to nothing)
- [x] Switch the bundle endpoint to the `https_spiffe` profile — no serving cert, CA, or key distribution needed
- [x] Provision a SPIRE agent live — co-located on the GCP SPIRE server VM, installed but not started; `forge-agent-join <token>` supplies the single-use join token at bootstrap
- [x] Provision `forge serve` on the AWS side — built from source alongside the AWS SPIRE server, crash-loops until the bootstrap bundle exchange completes, then self-starts
- [ ] Replace the build-from-source install of `forge serve` with a released binary once release automation exists
- [ ] Run the live bootstrap by hand following `docs/bootstrap-live.md`, and capture the transcript before automating any of it
- [ ] Confirm whether fck-nat's MASQUERADE rule SNATs tunnel traffic toward the AWS private subnets (layer 2 of the runbook)
- [ ] Post-provision SPIRE bootstrap — Phase 2 live: wire bootstrap against live GCP/AWS VMs, real upstream CA, cloud-native node attestors
- [ ] Bowtie licensing + initial admin bootstrap automation
- [ ] Provision instance secrets from a secret store rather than instance metadata — AWS SSM Parameter Store and GCP Secret Manager, read at boot via the instance profile / service account. Applies to the WireGuard private keys (currently in userdata, so readable via IMDS by anything on the box) and to the SPIRE bundle-endpoint serving key when F-01 lands.
- [ ] Revisit Bowtie for network-layer policy + PEP once federation is proven; resolve headless client enrollment with Bowtie support (browser/SSO login, device pre-authorization, client bundle contents)

## Low Priority (features & infra)
- [x] Implement AWS components (`pkg/components/aws/`)
- [ ] Add Temporal worker integration
- [x] Add policy-as-code via Pulumi CrossGuard
- [x] Add agent orchestration layer
- [ ] Create example stack config (`Pulumi.dev.yaml.example`)

## Security (from `docs/threat-model.md`)

### Done (runtime data plane + supply chain)
- [x] F-21: verify the SPIRE release download against the published `_sha256sum.txt` before install (fails closed)
- [x] F-03: structured `slog` audit on validations, authz decisions, and trust-root changes
- [x] F-04: generic client errors; no policy IDs leaked in `DenyReason`
- [x] F-05: rate limit + whole-request timeout on `forge serve`
- [x] F-06: `Valid` vs `Authorized` contract documented; partial authz requests fail closed
- [x] F-08: bundle continuity guard (refuse empty bundle; warn on root change)
- [x] F-09: honor `refreshHint`; track `LastRefresh`; surface staleness in `/healthz`
- [x] F-11: populate Cedar `EntityMap` with SPIFFE-derived principal attributes

### Open (no live-cloud / keystore dependency)
- [ ] F-02: TLS (ideally mTLS) in front of `forge serve`
- [ ] F-10: verify policy-file integrity at load
- [ ] F-14: enforce IMDSv2 on the AWS SPIRE instance (already required on the fck-nat launch template)
- [ ] F-18: Cloud SQL deletion protection + PITR
- [ ] F-19: scope SPIRE ingress to the VPC CIDR, not `10.0.0.0/8`
- [ ] F-20: restrict AWS SPIRE egress

### Phase-2 gates (blocked on live cloud / keystore — see threat model)
- [ ] F-15: move the SPIRE CA signing key off local disk to KMS (`gcp_kms`/`aws_kms`); exclude key material from disk snapshots
- [ ] F-16: set the real GCP OIDC TLS thumbprint on the AWS IAM OIDC provider (currently all-zeros placeholder)
- [x] F-01: retired for the default path — `https_spiffe` authenticates the endpoint with the server's own SVID, so no serving cert exists to provision. Still applies if `BundleProfileWeb` is ever selected.
- [ ] F-13: replace `join_token` node attestation with cloud-native attestors (gcp_iit / aws_iid)
- [ ] F-17: disable Cloud SQL public IPv4; private IP + authorized networks + SSL
