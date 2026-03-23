# Forge TODO

## High Priority (blocking federation)
- [x] Implement SPIFFE Bundle Endpoint refresh loop in `pkg/attestation` (RFC 9409)
- [x] Add SVID validation runtime code
- [x] Export fields in `TrustDomain`/`FederationPair` for serialization

## Medium Priority (tests & reliability)
- [x] Add tests for `pkg/config` (defaults, validation, edge cases)
- [ ] Add tests for `pkg/components/gcp` (Pulumi component tests)
- [ ] Add input validation in `pkg/config` (trust domain format, environment allowlist)

## Low Priority (features & infra)
- [ ] Implement AWS components (`pkg/components/aws/`)
- [ ] Add Temporal worker integration
- [ ] Add policy-as-code via Pulumi CrossGuard
- [ ] Add agent orchestration layer
- [ ] Create example stack config (`Pulumi.dev.yaml.example`)
