# Local Cross-Cloud Federation Demo (Phase 1)

Proves Forge's trust model end-to-end with **no cloud spend**: two federated
SPIRE servers standing in for GCP and AWS, where a JWT-SVID minted on the GCP
side is cryptographically validated on the AWS side through the real
`forge serve` / `pkg/attestation` code path.

> Why this matters: a workload identity issued in one cloud is verified in the
> other with no shared secret and no cross-cloud IAM trust. See
> [`../docs/why-this-model.md`](../docs/why-this-model.md).

## Prerequisites

- **Go 1.25+**
- A container runtime — either:
  - **Apple `container`** (default, macOS) — `container system status` should report `running`, or
  - **Docker** — run with `DEMO_RUNTIME=docker make demo`
- First run pulls the SPIRE 1.11.2 images (`ghcr.io/spiffe/spire-server`,
  `ghcr.io/spiffe/spire-agent`) and `alpine`; allow a few minutes. Subsequent
  runs are fast.

No cloud credentials, no network beyond the image pull, ~$0.

## Run it

```bash
make demo            # Apple `container` runtime (default)
DEMO_RUNTIME=docker make demo   # Docker fallback
make demo-clean      # tear down containers, network, generated artifacts
```

## What it stands up

```
            forge-demo network (one runtime, two trust domains)

  spire-gcp-server  ──issues JWT-SVID(aud=forge.aws.local)──>  spire-gcp-agent
  (forge.gcp.local)                                            (demo workload)
        │  https_web bundle endpoint :8443                          │ token
        │  (demo-CA cert, real RFC 9409 bundle)                     ▼
        ▼                                                   forge-serve  (AWS role)
   demo CA trusted via SSL_CERT_FILE  ───────────────────►  BundleRefresher GET (TLS)
                                                                    │
                                                                    ▼
                                          ValidateRemoteSVID → valid:true,
                                                               spiffe://forge.gcp.local/workload/demo
```

Containers: `spire-gcp-server`, `spire-aws-server`, `spire-gcp-agent`, and
`forge-serve` (a static linux `forge` binary in `alpine`, configured as the AWS
side). `forge-serve`, `BundleRefresher`, and `ValidateRemoteSVID` are unmodified
Forge code — the demo exercises the real validation path.

## What the bootstrap does (`bootstrap.sh`)

1. Wait for both SPIRE servers to report healthy.
2. **Federation:** exchange trust bundles between the servers (`bundle show` →
   `bundle set`), so each trusts SVIDs the other signs (RFC 9409).
3. Start `forge serve` (AWS role) once the GCP bundle endpoint is serving.
4. Generate an agent **join token** and launch the GCP agent with it.
5. Register a demo **workload entry** federated with the AWS trust domain.
6. **Mint** a JWT-SVID on the GCP side, audience `forge.aws.local`.
7. **Validate** it via `forge serve` `/validate` — expect `valid: true`.

## Expected output

A successful run ends like this (image-pull/progress lines elided):

```
==> rendering configs + certs
==> building forge linux binary
==> network forge-demo ready (container runtime)
==> starting SPIRE servers
==> resolving server IPs
  spire-gcp-server=192.168.65.2  spire-aws-server=192.168.65.3
==> running bootstrap
==> waiting for SPIRE servers to be healthy
  spire-gcp-server healthy
  spire-aws-server healthy
==> exchanging trust bundles (federation)
bundle set.
bundle set.
==> starting forge serve (AWS role) — GCP bundle endpoint is up now
  forge serve healthy
==> generating agent join token
  token: 0fdbf54b-396d-4af7-8a19-c60a8978d2b7
==> launching GCP agent with join token
  agent healthy
==> registering demo workload (federated with AWS)
Entry ID         : 1869ac62-8efc-4f03-9142-7f4ca9cee2b2
SPIFFE ID        : spiffe://forge.gcp.local/workload/demo
Parent ID        : spiffe://forge.gcp.local/agent
Selector         : unix:uid:0
FederatesWith    : forge.aws.local
==> minting JWT-SVID on GCP (audience = AWS trust domain)
==> validating the SVID through forge serve (AWS role)
validate response: {"valid":true,"spiffe_id":"spiffe://forge.gcp.local/workload/demo","trust_domain":"forge.gcp.local","expiry":"..."}
PASS: cross-cloud SVID validated (remote td=forge.gcp.local)
```

The final line is the proof: **`PASS: cross-cloud SVID validated`**. The
`make demo` exit code is `0` on success, non-zero on any failure.

## Files

| File | Role |
|------|------|
| `gen/main.go` | Renders the four SPIRE configs from `pkg/spire` into `generated/` |
| `gen-certs.sh` | Stamps a throwaway demo CA + `https_web` serving certs |
| `run.sh` | Starts the servers, resolves IPs, builds run-commands, invokes bootstrap |
| `bootstrap.sh` | Federation bundle exchange → join token → entry → mint → validate |
| `validate.sh` | POSTs the token to `forge serve /validate` and asserts the result |
| `docker-compose.yml` | Docker fallback service definitions |
| `integration_test.go` | `//go:build demo` smoke test that runs the whole thing |

`generated/` and `certs/` are runtime output (git-ignored).

## How the demo differs from a live deployment (Phase 2)

These are local-demo conveniences, not the production model:

- **`https_web` bundle endpoint + throwaway demo CA**, trusted via
  `SSL_CERT_FILE`. Live would use real web-PKI or SPIFFE-mTLS bundle endpoints.
- **`insecure_bootstrap`** on the agent (accepts the server on first connect).
  Live should pin a trust bundle.
- **`join_token` node attestation**. Live uses cloud-native attestors
  (`gcp_iit` / `aws_iid`).
- **`.local` trust domains** and a single host network. Live spans real
  GCP/AWS VPCs.

## Troubleshooting

- **`container system status` not running** → start Apple `container`, or use
  `DEMO_RUNTIME=docker make demo`.
- **Stuck / partial run** → `make demo-clean` and retry.
- **Inspect a component** → `container logs spire-gcp-server` (or
  `spire-gcp-agent`, `forge-serve`); `docker logs ...` on the Docker path.
