# Why This Trust Model Is Valid

Forge demonstrates cross-cloud workload authentication and authorization built
on SPIFFE/SPIRE federation. This note explains *why* the model is sound — the
claim the POC exists to prove.

```
                    what `make demo` proves
    ===========================================================

    IDENTITY LAYER  (SPIFFE / SPIRE)        <-- proven by make demo
    +-----------------------------------------------------------+
    |   forge.gcp.local                       forge.aws.local   |
    |   +---------------+   federation    +---------------+      |
    |   | SPIRE server  |<===============>| SPIRE server  |      |
    |   +---------------+  trust bundles   +---------------+      |
    |          | mints       (RFC 9409)           ^ verify       |
    |          v                                  | sig + aud    |
    |     [ workload ] ====== JWT-SVID =====> [ forge serve ]    |
    |                     aud=forge.aws.local         |          |
    |                                                 v          |
    |                                       {"valid": true}      |
    +-----------------------------------------------------------+
       no shared secret  .  no cross-cloud IAM trust
       identity proven by signature, not by network position

    NETWORK LAYER  (Bowtie mesh)            <-- model; Phase 2
    +-----------------------------------------------------------+
    |     GCP  <==========  encrypted path  ==========>  AWS    |
    +-----------------------------------------------------------+

    reachability =/= trust: a workload must REACH the service
    AND prove identity (SPIFFE) AND pass policy (Cedar)
```

## The problem

A workload in GCP needs to call a service in AWS. The traditional answers are
weak: long-lived API keys (leak, rarely rotated), static cloud IAM trust
(coarse, cloud-specific), or "it's on the trusted network so allow it" (network
position is not identity).

## The model

1. **Identity, not network position.** Each workload gets a SPIFFE ID
   (`spiffe://forge.gcp.local/workload/...`) backed by a short-lived,
   cryptographically verifiable SVID issued by its cloud's SPIRE server.
2. **Federation by trust-bundle exchange (RFC 9409).** The two SPIRE servers
   exchange signing bundles, so AWS can verify an SVID minted in GCP without any
   shared secret and without either cloud's IAM trusting the other directly.
3. **Audience-scoped, short-lived tokens.** A JWT-SVID is minted for a specific
   audience (the remote trust domain) and expires quickly — no standing
   credential to steal.
4. **Authorization is separate from authentication.** Proving identity (SPIFFE)
   and deciding access (Cedar ABAC) are distinct steps. A valid SVID is
   necessary but not sufficient.
5. **Reachability is decoupled from trust.** The Bowtie mesh provides the network
   path; SPIFFE provides identity. A workload must both reach the service *and*
   prove identity *and* pass policy — defense in depth across two CSPs.

## What the POC proves

`make demo` stands up two federated SPIRE servers (GCP and AWS roles), mints a
JWT-SVID for a GCP workload audienced to the AWS trust domain, and validates it
through `forge serve` — the same code path the live deployment uses. If the same
configs federate locally, the cloud version is networking glue, not model risk.

## What is deliberately deferred (Phase 2)

Live GCP/AWS provisioning, cloud-native node attestation (`gcp_iit`/`aws_iid`),
KMS-backed upstream CA, and SPIFFE-mTLS bundle endpoints. None change the model;
they harden the transport and attestation around it.
