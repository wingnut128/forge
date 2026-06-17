# Forge Threat Model — STRIDE

Status: **POC / Phase 1**. Scope: the cross-cloud SPIFFE trust model Forge exists to
prove, plus the infrastructure that provisions it.

## Method

Each element of the attack surface is classified with **STRIDE-per-element**: a closed
walk through Spoofing, Tampering, Repudiation, Information-disclosure, Denial-of-service,
and Elevation-of-privilege — each category being the negation of one CIA+ property
(authenticity, integrity, non-repudiation, confidentiality, availability, authorization).
An element is "classified" only once all six categories have been checked, even where a
category is N/A. Findings are grounded in `file:line` and collected in the
[register](#findings-register) at the end. STRIDE gives completeness over the *taxonomy*,
not over real-world threats — business-logic, chained, and supply-chain risks need
separate attention.

## System decomposition

```
                        ┌─────────────────────── GCP trust domain ───────────────────────┐
                        │  SPIRE server VM (CA, bundle endpoint :8443, gRPC :8081)         │
                        │   ├─ CA signing key  (KeyManager "disk" → keys.json on data disk)│
   remote workload      │   ├─ DataStore (sqlite on disk | Cloud SQL Postgres)            │
   (AWS trust domain) ──┼──▶ bundle endpoint  ──[https_web]──▶ trust bundle               │
        │   ▲           │   └─ daily disk snapshots (7-day retention)                      │
        │   │           │  WIF pool/provider (accepts AWS SPIRE OIDC JWTs)                 │
   JWT-SVID │ bundle    │  Bowtie controller VM (admin :22/:443, mesh :51820)             │
        │   │           └─────────────────────────────────────────────────────────────────┘
        ▼   │
   ┌─────────────────── forge serve (attestation data plane) ───────────────────┐
   │  POST /validate ── ValidateRemoteSVID ── BundleRefresher (cached trust root) │
   │       └─ optional Cedar authz ── policy files (FORGE_POLICY_DIR)             │
   │  GET  /healthz                                                               │
   └─────────────────────────────────────────────────────────────────────────────┘
        (mirror on the AWS side: IAM OIDC provider accepts GCP SPIRE JWTs)
```

**Trust boundaries.** (1) remote cloud → token/bundle ingress into `forge serve`;
(2) public internet → SPIRE bundle endpoint / Bowtie mesh; (3) operator-controlled
policy + key material → the trusting processes; (4) cloud-managed-service boundary
(Cloud SQL / KMS / Secret Manager).

## Element inventory

| # | Element | Kind | Plane |
|---|---------|------|-------|
| 0 | SPIRE bundle endpoint flow (`FORGE_BUNDLE_ENDPOINT_URL`) | data flow | runtime |
| A | `/validate` endpoint | process | runtime |
| B | JWT-SVID token in transit | data flow | runtime |
| C | `BundleRefresher` + cached bundle | process + store | runtime |
| D | Cedar policies + authorizer | store + process | runtime |
| E | `/healthz` endpoint | process | runtime |
| I1 | SPIRE server VM/EC2 (CA + bundle endpoint host) | process | infra |
| I2 | CA signing key on disk + snapshots | data store | infra |
| I3 | Federation acceptors (GCP WIF, AWS IAM OIDC) | trust anchor | infra |
| I4 | Managed state (Cloud SQL/RDS, KMS, Secret Manager) | data store | infra |
| I5 | Network boundaries (firewalls / security groups) | boundary | infra |
| I6 | Bowtie controller | process | infra |
| I7 | SPIRE provisioning pipeline (startup/user-data script) | process | infra |

---

## Runtime data plane

### 0 · SPIRE bundle endpoint flow
GCP SPIRE → AWS SPIRE bundle fetch (and mirror). `BundleRefresher.fetch`, `pkg/attestation/bundle.go`.

- **S** authenticity — partner fetches the bundle over `https_web` (`pkg/spire/config.go:77`): endpoint identity rests on a **web-PKI serving cert**. A rogue endpoint (DNS/BGP hijack, MITM) serving a forged bundle → attacker-controlled SVIDs trusted. The endpoint's serving cert (`/etc/spire/certs/server.{crt,key}`) is **never provisioned by the startup script** (only the dir is created, `config.go:241`) → live federation has no serving identity until an out-of-band step. **[F-01]**
- **T** integrity — bundle alterable at rest on the VM (see I2); a downgraded refresh/sequence keeps a stale bundle past rotation.
- **R** non-repudiation — bundle fetches/swaps unlogged (see C).
- **I** confidentiality — bundle is public key material; low. Endpoint leaks trust-domain names / server version.
- **D** availability — endpoint down at refresh ⇒ partner's cached anchor expires ⇒ federation outage. No rate-limit on the bundle handler.
- **E** authorization — poisoned anchor cashes out at I3 → SVIDs valid in *both* clouds.

### A · `/validate` endpoint — `pkg/orchestration/server.go:109`
- **S** — plain `http.Server` (`:47`), **no TLS/mTLS or client auth**; anyone reaching the listener calls `/validate`. **[F-02]**
- **T** — 1MB body cap (`:115`); `action`/`resource` are attacker-controlled into Cedar.
- **R** — **no logging of any validation or authz decision.** **[F-03]**
- **I** — raw `err.Error()` returned to caller (`:128`); `DenyReason` leaks policy internals (`:147`). **[F-04]**
- **D** — no rate limit, no whole-request timeout (only `ReadHeaderTimeout`); crypto+Cedar per call. **[F-05]**
- **E** — **authz opt-out:** `authorizer==nil` *or* empty `action`/`resource` ⇒ `Valid:true` with no authorization (`:139`). Unsafe if a consumer reads `Valid:true` as "allowed." (Cedar itself is default-deny, `pkg/authz/authz.go:95`.) **[F-06]**

### B · JWT-SVID token in transit
- **S/T** — signature + audience + trust-domain pinned (`pkg/attestation/validate.go:16,20`); forgery reduces to bundle poisoning. Defended.
- **R** — **bearer token, no replay protection** (no `jti`/nonce); captured token replays until `exp`. **[F-07]**
- **I** — if the listener is plaintext (F-02), the token is sniffable → impersonation until expiry. **[F-02]**
- **D/E** — size-capped; audience+TD pin block cross-audience elevation. Residual: any workload in the remote TD validates (per-ID gating pushed entirely to Cedar).

### C · BundleRefresher + cached bundle — `pkg/attestation/bundle.go`
- **T** — swapped under lock (`:75`), but a failed refresh **silently keeps the old bundle** (`:95`) → revoked anchors linger; **no continuity/pinning check**, so one poisoned fetch replaces the whole root. **[F-08]**
- **R** — failures logged (`:96`); successful **swaps unlogged** — can't audit when the trust root changed. **[F-03]**
- **D** — **initial fetch failure is fatal** (`:84`); stale bundle served indefinitely if the endpoint stays down; fixed 5-min interval **ignores `refreshHint`**. **[F-09]**
- **S/I/E** — endpoint identity per element 0; bundle is public; poison cashes out at I3.

### D · Cedar policies + authorizer — `pkg/authz/authz.go`
- **T** — loaded once at startup, never reloaded (runtime file tamper inert until restart — good), but **no integrity check** on `.cedar` files; a tampered `permit(...)` is ingested wholesale. **[F-10]**
- **R** — decisions unlogged (F-03).
- **I** — `DenyReason` returns policy IDs / diagnostic messages to an unauthenticated caller (`:96`). **[F-04]**
- **E** — default-deny confirmed (`:95`); but **empty `EntityMap{}`** (`:64`) means attribute/parent-based policies silently see no entity data and mis-evaluate. **[F-11]**
- **S/D** — operator-trusted source; malformed policy fails closed at startup.

### E · `/healthz` endpoint — `pkg/orchestration/server.go:154`
- **I** — returns `"no bundle loaded"` vs `"ok"` (`:157`) → discloses federation-outage state to an unauthenticated probe. **[F-12]**
- **S/T/R/D/E** — unauthenticated cheap GET; low.

---

## Infrastructure / provisioning plane

### I1 · SPIRE server VM/EC2 — `pkg/components/{gcp,aws}/spire_server.go`
The CA and bundle-endpoint host; compromise = total trust-domain compromise.
- **S** — node enrollment uses **`join_token`** attestation (`pkg/spire/config.go:96`); join tokens are bearer secrets — a leaked/guessed token lets a rogue agent enroll. No cloud-native node attestor yet (Phase 2). **[F-13]**
- **T** — GCP: Shielded VM secure-boot + integrity monitoring on (`gcp/spire_server.go:111`). AWS: **no IMDSv2 enforcement / equivalent** specified. Config written by root metadata/user-data script. **[F-14]**
- **R** — no host-level audit shipping; SPIRE logs local to the VM disk only.
- **I** — see I2 (key + datastore on disk; snapshots).
- **D** — single VM per cloud, no HA; daily snapshots give recovery, not availability. Acceptable for POC.
- **E** — bundle-endpoint/gRPC reachable from `10.0.0.0/8` (see I5); host compromise → mint arbitrary SVIDs.

### I2 · CA signing key on disk + snapshots
- **T/I** — **CA signing key stored as a plaintext file** (`KeyManager "disk"`, `keys_path=.../keys.json`, `pkg/spire/config.go:90`) on the data disk — *not* the KMS key that managed-state provisions (`gcp/managed_state.go:75` is created but unreferenced). Daily disk snapshots (`gcp/spire_server.go:56`) **copy the signing key into 7 days of snapshots**, widening the disclosure surface. AWS EBS data volume is encrypted (`aws/spire_server.go:97`); GCP data disk uses default Google-managed encryption (no CMEK). **[F-15]**
- **E** — possession of `keys.json` = ability to mint SVIDs trusted across both clouds. Highest-value asset in the system.
- **S/R/D** — n/a beyond the above.

### I3 · Federation acceptors (GCP WIF, AWS IAM OIDC)
- **S** — **AWS IAM OIDC provider ships a placeholder all-zeros thumbprint** (`aws/spire_oidc.go:51`): the GCP SPIRE OIDC endpoint's TLS cert is **not actually pinned**, so the AWS→GCP-issuer trust is unverified as written. GCP WIF validates the AWS issuer's JWKS over standard TLS (no manual thumbprint), so the GCP side is sound; the AWS side is not. **[F-16]**
- **T** — WIF `AttributeCondition` restricts subjects to the expected trust domain (`gcp/workload_identity.go:82`); audience pinned (`:67`). Good. No equivalent subject-scoping condition on the AWS OIDC provider beyond `ClientIdList`.
- **E** — over-broad acceptance: trusting the partner's *entire* trust domain rather than specific SPIFFE IDs → confused-deputy if downstream IAM role trust policies aren't tightly scoped.
- **R/I/D** — federation config changes audited via cloud provider logs (out of band).

### I4 · Managed state (Cloud SQL / RDS, KMS, Secret Manager)
Opt-in (`enable-managed-state`); **currently a stub** — the rendered DSN is hardcoded
`postgres://spire@127.0.0.1:5432/spire` with no password (`gcp/spire_server.go:162`,
`aws/spire_server.go:134`), so the SQL/KMS/Secret resources are provisioned but unwired.
- **I/T/E** — **Cloud SQL has a public IPv4 enabled** (`gcp/managed_state.go:48`) — the SPIRE datastore (registration entries) is reachable from the internet, gated only by authorized-networks/SSL (not configured here). **[F-17]**
- **S** — Secret Manager admin-token secret created **with no version/value** (`:86`) — placeholder; join token not actually delivered via the secret.
- **D** — `DeletionProtection:false` (`:44`) and `PointInTimeRecoveryEnabled:false` (`:52`) — easy accidental/ malicious data loss, limited recovery. **[F-18]**
- **R** — KMS/SQL access auditable via cloud logs (out of band).

### I5 · Network boundaries (firewalls / security groups)
- **E/S** — SPIRE ingress allowed from **`10.0.0.0/8`** (entire RFC-1918 /8, not just the VPC CIDR) on both clouds (`gcp/spire_server.go:134`, `aws/spire_server.go:59`); GCP `allow-internal` opens `0-65535` within the VPC. Broader than needed. **[F-19]**
- **I** — AWS SPIRE SG **egress is `0.0.0.0/0` all-protocols** (`aws/spire_server.go:67`) — unrestricted exfil path from a host holding the CA key. **[F-20]**
- **D** — no per-element rate limiting at the network edge.
- **T/R** — n/a.

### I6 · Bowtie controller — `pkg/components/{gcp,aws}/bowtie.go`
- **S/E** — admin ports (22/443) locked to `AdminCIDRs`, **defaulting to `127.0.0.1/32`** when unset (`gcp/bowtie.go:93`) — fail-closed, good. WireGuard mesh (51820) open to `0.0.0.0/0` (`:128`) — internet-facing but cryptographically authenticated by design.
- **I/D/T/R** — admin plane out of the federation trust path; licensing/bootstrap is Phase 2.

### I7 · SPIRE provisioning pipeline (startup / user-data script)
- **T** — `RenderServerStartupScript` **downloads the SPIRE release over `curl -sSL` with no checksum or signature verification** (`pkg/spire/config.go:236`), then installs and runs it as root. A compromised release host or MITM ⇒ arbitrary code as root on the CA host. **[F-21]**
- **S/R** — script runs from cloud metadata (provider-authenticated); no record of which binary was installed.
- **I/D/E** — n/a beyond the above.

---

## Findings register

Severity is relative to a **live** deployment; many are acceptable for the local POC
(`make demo`) and called out as Phase-2 gates.

| ID | Element | STRIDE | Severity | Status | Recommendation |
|----|---------|--------|----------|--------|----------------|
| F-01 | 0 | S/T | High | Phase-2 gate | Provision the bundle-endpoint serving cert; consider `https_spiffe` profile to anchor on SPIFFE rather than web PKI |
| F-02 | A/B | S/I | High | Open | Terminate TLS (ideally mTLS) in front of `forge serve`; never expose `/validate` plaintext |
| F-03 | A/C/D | R | High | ✅ Fixed | Structured `slog` audit on every validation, authz decision, and trust-root change |
| F-04 | A/D | I | Medium | ✅ Fixed | Generic client errors; detail logged server-side; `DenyReason` no longer leaks policy IDs |
| F-05 | A | D | Medium | ✅ Fixed | Global token-bucket rate limit + `http.TimeoutHandler` whole-request timeout |
| F-06 | A | E | High | ✅ Fixed | `Valid` vs `Authorized` contract documented; partial authz requests fail closed (400) |
| F-07 | B | R | Medium | Accepted (SPIFFE) | Document bearer-token replay window; shorten SVID TTL; consider one-time `jti` cache |
| F-08 | C | T | Medium | ✅ Fixed | Continuity guard refuses an empty bundle; root-authority changes logged at WARN |
| F-09 | C | D | Medium | ✅ Fixed | Honors `refreshHint` (floored); tracks `LastRefresh`; staleness surfaced in `/healthz` |
| F-10 | D | T | Medium | Open | Verify policy file integrity (checksum/signature) at load |
| F-11 | D | E | Medium | ✅ Fixed | Principal entity registered with `trust_domain`/`path` attrs parsed from the SPIFFE ID |
| F-12 | E | I | Low | Accepted | Acceptable for a liveness probe; restrict if endpoint becomes public |
| F-13 | I1 | S | High | Phase-2 gate | Replace `join_token` with cloud-native node attestors (gcp_iit / aws_iid) |
| F-14 | I1 | T | Medium | Open | Enforce IMDSv2 on AWS; minimize root startup-script surface |
| F-15 | I2 | T/I/E | **High** | Phase-2 gate | Use the provisioned KMS key (`KeyManager "gcp_kms"` / `aws_kms`); exclude key material from snapshots; CMEK on GCP disk |
| F-16 | I3 | S | **High** | Phase-2 gate | Set the real GCP OIDC TLS thumbprint; verify it in CI |
| F-17 | I4 | I/T/E | High | Phase-2 gate | Disable Cloud SQL public IPv4; use private IP + authorized networks + SSL |
| F-18 | I4 | D | Medium | Open | Enable deletion protection + PITR before any real data |
| F-19 | I5 | E/S | Medium | Open | Scope SPIRE ingress to the actual VPC CIDR, not `10.0.0.0/8` |
| F-20 | I5 | I | Medium | Open | Restrict AWS SPIRE egress to required destinations |
| F-21 | I7 | T | High | ✅ Fixed | SPIRE archive verified against the published `_sha256sum.txt` (TLS-pinned curl) before install; fails closed |

## Out of scope / limitations

- **GKE / EKS clusters** (`enable-gke`/`enable-eks`) and the Kubernetes workload-attestation
  path are not yet walked — they are opt-in and orthogonal to the VM-based trust proof.
- **Demo harness** (`demo/`) uses intentionally insecure shortcuts (`insecure_bootstrap`,
  self-signed certs) and is explicitly non-production.
- Threats are taxonomy-complete per element, **not** exhaustive of business-logic or
  multi-step chains. The two dominant chains here are *rogue endpoint → poisoned/disk-stolen
  key → SVIDs valid in both clouds* (0 → I2 → I3) and *request-level authz opt-out* (A).

## References
- `pkg/orchestration/server.go`, `pkg/attestation/`, `pkg/authz/authz.go` (runtime)
- `pkg/components/{gcp,aws}/`, `pkg/spire/config.go` (infrastructure)
- [`docs/why-this-model.md`](why-this-model.md) — why this trust model
