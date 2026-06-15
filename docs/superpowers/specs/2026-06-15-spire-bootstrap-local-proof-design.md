# SPIRE Bootstrap — Phase 1: Local Cross-Cloud Federation Proof

**Date:** 2026-06-15
**Status:** Approved (design)
**Scope owner:** Forge POC track

## Context

Forge provisions SPIRE servers on GCP and AWS, bidirectional SPIFFE federation
wiring, RFC 9409 bundle refresh (`pkg/attestation`), JWT-SVID validation, and
Cedar ABAC. But no SVID has ever crossed a cloud boundary: the SPIRE server VMs
boot with a **placeholder** `server.conf` (trust domain + bind config only — no
`federation{}` block, no datastore/keymanager tuning), and the bootstrap step
that arms federation at runtime is unbuilt.

Forge is a **proof-of-concept / portfolio artifact, not a platform**. The goal
of this work is the thinnest end-to-end slice that *proves the cross-cloud trust
model is valid* and *articulates why it is valid* — not a production system.
Every feature outside that slice is frozen (see Out of Scope).

This is **Phase 1: local proof**. Phase 2 (live GCP + AWS) is deferred. The
design is shaped so Phase 1 artifacts (the config generator) are reused, not
rebuilt, in Phase 2 — "same configs, live later." If the same configs federate
locally, the cloud version is networking glue, not model risk.

## Success Criterion

A single command (`make demo`) stands up two federated SPIRE servers
representing GCP and AWS, mints a JWT-SVID for a workload on the "GCP" side with
audience = the AWS trust domain, and `forge serve` (acting as the AWS side)
validates it through the **existing** `pkg/attestation` / `/validate` code path,
returning `valid: true` with the GCP SpiffeID.

Constraints: reproducible by anyone with Apple `container` (or Docker as a
fallback), `$0`, no cloud credentials, and
it exercises the real validation code (`BundleRefresher`, `ValidateRemoteSVID`,
`orchestration.Server`) unchanged.

## Architecture

Three pieces, with a clean dependency direction: the demo harness depends on the
config generator; the VM components depend on the config generator; nothing
depends on the demo.

```
            pkg/spire (config generator)
              ▲                    ▲
              │                    │
   pkg/components/{gcp,aws}      demo/ (docker-compose + bootstrap)
   (VM startup scripts)         (local Phase 1 proof)
```

### Component 1 — `pkg/spire/` (new package)

Venue-agnostic generation of SPIRE configuration. This is the heart of "same
configs, live later": both the VM startup scripts and the local demo render
config from here.

- `ServerConfig` struct — trust domain, peer trust domain, peer bundle endpoint
  URL, own bundle endpoint bind, state mode (disk | managed), data dir.
- `RenderServerHCL(ServerConfig) (string, error)` — produces a federation-aware
  `server.conf`:
  - `server{}` block (bind, trust_domain, data_dir).
  - `federation{}` block: own `bundle_endpoint{}` plus
    `federates_with "<peer-td>" { bundle_endpoint_url = ...; bundle_endpoint_profile = ... }`.
  - Plugins selected by state mode: disk-backed `DataStore "sql"` (sqlite) +
    `KeyManager "disk"` for the default track; the managed branch leaves
    placeholders for Cloud SQL/RDS + KMS (Phase 2, not rendered live now but the
    branch exists so the shape is correct).
- `AgentConfig` struct + `RenderAgentHCL(AgentConfig) (string, error)` —
  `agent.conf` with trust domain, server address, and a join-token node
  attestor (sufficient for the demo; production node attestors are Phase 2).

Pure functions, no I/O. Fully unit-testable with golden files.

### Component 2 — `demo/` (new directory)

The local harness. Consumes `pkg/spire` for config; owns no SPIRE config of its
own (no hand-maintained HCL).

Runtime target: **Apple `container`** (the macOS containerization CLI) is the
primary runtime — it has stronger per-container isolation (one lightweight VM
each) and cleaner networking than Docker Desktop on macOS. **Docker / `docker
compose` is a documented fallback** so the demo runs for anyone without
`container`. Both runtimes consume identical, `pkg/spire`-rendered config and the
same `bootstrap.sh`; only the thin orchestration layer forks.

Nodes (same under either runtime):
  - `spire-gcp-server`, `spire-gcp-agent`
  - `spire-aws-server`, `spire-aws-agent`
  - trust domains: `forge.gcp.local` and `forge.aws.local`.

No bundle-publisher sidecar. Each SPIRE server exposes its **own native RFC 9409
bundle endpoint** over TLS using the `https_web` profile, served with a cert
issued by a throwaway **demo CA** generated at instantiation (see Bundle Endpoint
TLS below). `forge serve`'s `BundleRefresher` fetches that endpoint directly; the
demo CA, installed in the `forge serve` container's system trust store, makes the
default `http.Client` trust it with no runtime code change.

Orchestration:
  - Primary: `demo/run.sh` driving the `container` CLI — `container network
    create forge-demo`, then one `container run` per node on that shared network,
    with explicit readiness polls (no compose `depends_on`).
  - Fallback: `demo/docker-compose.yml` for the Docker path.
  - `make demo` selects the runtime (prefers `container`, falls back to Docker)
    and runs `bootstrap.sh`.
- `bootstrap.sh`: the bootstrap sequence (see Bootstrap Flow). Runtime-agnostic —
  it `exec`s into running containers by name, which both runtimes support.
- Config files rendered into the run context from `pkg/spire` via a small
  generator entrypoint (e.g. `go run ./demo/gen` or a `make` target) so the
  rendered HCL is never hand-edited.

#### Bundle Endpoint TLS (demo CA + `https_web`)

The bundle endpoints are served over real TLS, not plain HTTP, using a local
chain stamped at instantiation. This uses SPIRE's actual bundle endpoint and
requires no change to `BundleRefresher`.

- `demo/gen-certs.sh` (run by `make demo` before bring-up): generate a throwaway
  **demo CA**, then issue one server cert per SPIRE bundle endpoint with SAN =
  the server's hostname on the `forge-demo` network (`spire-gcp-server`,
  `spire-aws-server`).
- Each SPIRE server's `federation { bundle_endpoint { ... } }` is configured with
  `profile = "https_web"` and pointed at its issued cert/key. Each
  `federates_with` peer entry likewise uses `https_web`.
- The demo CA is installed into the system trust store of **three** container
  roles: both SPIRE servers (so each trusts the peer's `https_web` endpoint when
  `federates_with` fetches the peer bundle) and the `forge serve` container (so
  `BundleRefresher`'s default `http.Client` trusts the endpoint). Trust is
  injected at the OS layer — no Go code change.
- **Two distinct trust layers, do not conflate:** the demo CA secures the
  *transport* (TLS to the bundle endpoint); the SPIFFE trust bundle exchanged in
  bootstrap step 2 carries the *SVID-signing* keys. The `https_web` choice (vs
  `https_spiffe`, which would need a `BundleRefresher` mTLS change) is what keeps
  Phase 1 free of runtime code changes.

### Component 3 — Documentation

- This design doc.
- `docs/why-this-model.md` — the "why it's a valid model" narrative: workload
  identity decoupled from network reachability, federation via short-lived
  cryptographic SVIDs instead of shared secrets or static cloud IAM trust, and
  why that is the right trust primitive for multi-CSP. This is a first-class
  deliverable, not an afterthought.

## Bootstrap Flow (`bootstrap.sh`)

This script is the concrete realization of the long-standing TODO
("post-provision SPIRE bootstrap: federation registration, join tokens").

1. Wait for both SPIRE servers to report healthy.
2. **Bundle exchange** (solves the federation chicken-and-egg):
   `spire-server bundle show` on GCP → `spire-server bundle set -federatesWith
   forge.gcp.local` on AWS, and symmetrically AWS → GCP. After this initial
   exchange, the `federates_with` config keeps bundles refreshed automatically.
3. **Registration entry:** create a workload entry on the GCP server, federated
   with the AWS trust domain (`-federatesWith forge.aws.local`), with a selector
   the demo agent/workload satisfies.
4. **Agent join token:** generate a join token on the GCP server, start the GCP
   agent with it.
5. **Mint SVID:** fetch a JWT-SVID for the registered workload with
   `-audience forge.aws.local`.
6. **Validate:** POST the token to `forge serve` `/validate` (configured with
   `FORGE_LOCAL_TRUST_DOMAIN=forge.aws.local`,
   `FORGE_REMOTE_TRUST_DOMAIN=forge.gcp.local`,
   `FORGE_BUNDLE_ENDPOINT_URL=https://spire-gcp-server:8443`). Assert
   `valid: true` and the SpiffeID is `spiffe://forge.gcp.local/...`.

## Data Flow

```
spire-gcp-server ──issues JWT-SVID(aud=forge.aws.local)──> demo workload
        │                                                     │ token
        │ native bundle endpoint                              ▼
        │ (https_web, demo-CA cert)              forge serve (role: AWS)
        └──────────────────────────────────┐       │ BundleRefresher GET (TLS,
                                            ▼       ▼   demo CA in trust store)
                          serves SPIFFE bundle JSON ─┘
                                            │
                                            ▼
                          ValidateRemoteSVID → valid:true,
                                               spiffe://forge.gcp.local/workload
```

`forge serve`, `BundleRefresher`, and `ValidateRemoteSVID` are all existing,
unchanged Forge code; trust in the endpoint cert comes from the demo CA installed
in the container's OS trust store.

## Key Design Decisions

- **Native `https_web` bundle endpoint + demo CA, over a publisher sidecar.**
  `BundleRefresher` does a plain `http.Client` GET and `spiffebundle.Parse`.
  SPIRE's native `https_spiffe` bundle endpoint requires SPIFFE mTLS, which the
  current client does not speak. Rather than either (a) modify the runtime to
  speak mTLS or (b) serve the bundle over plain HTTP via a sidecar, the demo
  stamps a throwaway CA at instantiation, serves each SPIRE server's real bundle
  endpoint with `https_web`, and installs the CA in the relevant container trust
  stores. This uses SPIRE's actual endpoint, drops the sidecar, keeps
  `BundleRefresher` unchanged (trust injected at the OS layer), and exercises the
  web-PKI transport shape Phase 2 will use — at the cost of a cert-generation step
  and CA injection into three container roles. `https_spiffe` is the more
  production-faithful endgame but is deferred because it requires a
  `BundleRefresher` change.
- **Config generator is the shared seam.** Putting rendering in `pkg/spire` means
  the VM startup scripts and the demo cannot drift, and Phase 2 inherits the
  federation-aware config for free.
- **Join-token node attestation for the demo.** Simplest attestor that proves the
  full path; cloud-native attestors (`gcp_iit`, `aws_iid`) are Phase 2.
- **Two `.local` trust domains.** Keeps the demo self-contained and avoids any
  dependence on real DNS or cloud endpoints.

## VM Startup-Script Refactor

`pkg/components/gcp/spire_server.go` and `pkg/components/aws/spire_server.go`
currently embed near-identical placeholder `server.conf` HCL in their startup
scripts. Both are refactored to render config via `pkg/spire.RenderServerHCL`,
removing the duplication and the placeholder. The startup scripts keep their
disk-mount / binary-install / systemd responsibilities; only the config block is
sourced from `pkg/spire`. This is targeted improvement of code we are already
touching — not unrelated refactoring.

## Testing

- **Unit (fast, in `go test ./...`):** golden-file tests on `RenderServerHCL` and
  `RenderAgentHCL` — assert the `federates_with` peer/URL/profile, mode-driven
  datastore/keymanager selection, and stable output.
- **Integration (opt-in):** a `make demo` smoke test that runs the full
  compose + bootstrap and asserts `/validate` returns `valid: true`. Gated behind
  a build tag and Docker availability so the default `go test ./...` stays fast
  and hermetic.

## Out of Scope (frozen — defer to Phase 2+)

Live GCP/AWS provisioning; real IAM OIDC thumbprints; GKE/EKS; managed-state
runtime (Cloud SQL/RDS/KMS); Bowtie mesh; upstream CA via real KMS;
cloud-native node attestors; multiple workloads; pluggable policy storage;
Temporal. **If any of these tempt us mid-build, we stop and defer rather than
widen the lane.**

## Documentation Updates (same PR)

Per repo rules, update in the same change: `README.md` (add the local demo +
`make demo`), `CLAUDE.md` (new `pkg/spire` layout + demo commands), `TODO.md`
(check off the bootstrap item's Phase 1 portion, note Phase 2 remainder).
