# Live bootstrap runbook

How to go from an empty stack to a validated cross-cloud SVID, and how to tell
which layer broke when it doesn't work.

**Bring the layers up in order and verify each one before moving on.** Four
things are unproven against real infrastructure — the WireGuard tunnel, the
pinned addressing, `https_spiffe`, and agent attestation. If you deploy
everything and test only at the end, a failure gives you four suspects at once.

The pass criterion for the whole exercise is one thing:

> A JWT-SVID minted by the SPIRE server on the **GCP** VM is validated by
> `forge serve` on the **AWS** side, using a trust bundle fetched over the real
> cross-cloud network path.

A green `pulumi up` is a precondition, not the criterion.

---

## Layer 0 — configure

Generate two WireGuard keypairs. They never leave your machine except as stack
config, which Pulumi encrypts.

```bash
GCP_PRIV=$(wg genkey); GCP_PUB=$(printf '%s' "$GCP_PRIV" | wg pubkey)
AWS_PRIV=$(wg genkey); AWS_PUB=$(printf '%s' "$AWS_PRIV" | wg pubkey)

pulumi config set --secret forge:wg-gcp-private-key "$GCP_PRIV"
pulumi config set         forge:wg-gcp-public-key  "$GCP_PUB"
pulumi config set --secret forge:wg-aws-private-key "$AWS_PRIV"
pulumi config set         forge:wg-aws-public-key  "$AWS_PUB"
pulumi config set forge:enable-vpn true
```

Trust domains are **identifiers, not addresses** — nothing resolves them, so
pick anything stable:

```bash
pulumi config set forge:environment           dev
pulumi config set forge:spire-trust-domain     forge.dev.gcp
pulumi config set forge:aws-spire-trust-domain forge.dev.aws
pulumi config set forge:spire-aws-ami "$(aws ssm get-parameter \
  --region us-east-1 \
  --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --query Parameter.Value --output text)"
pulumi config set gcp:project <YOUR_PROJECT_ID>
pulumi config set aws:region us-east-1   # must match forge:aws-region
```

Confirm the expensive flags are off before deploying:

```bash
pulumi config | grep -E 'enable-(gke|eks|managed-state|multi-az-nat|bowtie)'
```

**Set a teardown reminder now, not later.** The steps below involve waiting on
boots and manual verification; that is exactly when a stack gets forgotten
overnight.

---

## Layer 1 — deploy

```bash
FORGE_STACK=dev go run ./cmd/forge preview   # sanity-check the resource count
FORGE_STACK=dev go run ./cmd/forge up
```

Expect 8–14 minutes. The AWS NAT instance dominates.

**Verify:** both SPIRE VMs and the GCP gateway exist, and the SPIRE VMs have no
public IP.

```bash
gcloud compute instances list --filter="labels.environment=dev"
aws ec2 describe-instances \
  --filters Name=tag:forge:component,Values=spire-server \
  --query 'Reservations[].Instances[].[InstanceId,State.Name,PrivateIpAddress]' --output table
```

The AWS SPIRE server must report `10.1.0.10`, and the GCP one `10.0.16.10`. If
either differs, the pinned-address assumption is broken and nothing downstream
will work.

---

## Layer 2 — the tunnel

This is the layer most likely to fail first, and the one everything else rests
on. Verify it **before** looking at SPIRE at all.

Get onto the boxes. Neither has an SSH key; IAP and SSM are the only routes in:

```bash
# GCP gateway (has a public IP) or the SPIRE server (does not — IAP handles both)
gcloud compute ssh forge-dev-vpn --tunnel-through-iap --zone us-central1-a

# AWS NAT instance — find it via the ASG, then connect through SSM
aws ec2 describe-instances \
  --filters Name=tag:forge:component,Values=fck-nat Name=instance-state-name,Values=running \
  --query 'Reservations[].Instances[].InstanceId' --output text
aws ssm start-session --target <instance-id>
```

**Check the tunnel came up.** On either gateway:

```bash
sudo wg show
```

A working tunnel shows the peer's public key, an endpoint, and a recent
`latest handshake`. **No handshake means the tunnel never established** — check,
in this order: the security group / firewall allows UDP 51820 from the *peer's*
public IP; the public IPs in each side's config match what was actually
allocated; and the keys are not swapped (each side needs the *other's* public
key, not its own).

**Check packets actually cross:**

```bash
# from the GCP gateway
ping -c3 10.99.0.2          # AWS tunnel endpoint
ping -c3 10.1.0.10          # AWS SPIRE server, through the tunnel

# from the AWS NAT instance
ping -c3 10.99.0.1
ping -c3 10.0.16.10
```

If the `/30` pings work but the VPC-address pings do not, it is a **routing or
forwarding** problem, not a WireGuard problem. Check `AllowedIPs` covers the
peer's VPC CIDR on both sides, that `net.ipv4.ip_forward=1`, and that the
GCE route plus the AWS private route table point at the right next hop.

> **Known open question.** fck-nat's MASQUERADE rule may SNAT tunnel traffic
> heading for the AWS private subnets, so the GCP SPIRE server could appear to
> originate from the NAT instance's address. Federation still functions if so,
> but confirm what the AWS SPIRE server actually sees before concluding the
> tunnel is clean.

**Do not continue until both VPC-address pings succeed in both directions.**

---

## Layer 3 — SPIRE servers

```bash
sudo systemctl status spire-server
sudo /usr/local/bin/spire-server healthcheck
```

If the service is dead, read `journalctl -u spire-server` and the cloud-init
log. The most likely first-boot failure is the release download — the VM can
boot before the NAT instance has iptables up, though the script now retries for
~50s.

**Reachability across the tunnel** (the thing federation depends on):

```bash
# from the AWS SPIRE server
curl -k -sS https://10.0.16.10:8443 | head -c 200
```

Anything other than a connection error means the endpoint is live. A TLS
complaint is fine here — `curl` has no reason to trust a SPIFFE SVID; SPIRE
will.

---

## Layer 4 — exchange trust bundles

**This must happen before the first federated fetch.** `https_spiffe` validates
the peer's endpoint against a bundle it already holds, so with no seeded bundle
both sides deadlock.

```bash
# GCP bundle -> AWS server
sudo spire-server bundle show -format spiffe > /tmp/gcp.bundle   # on GCP
# copy /tmp/gcp.bundle to the AWS server, then:
sudo spire-server bundle set -format spiffe -id spiffe://forge.dev.gcp < /tmp/gcp.bundle

# AWS bundle -> GCP server
sudo spire-server bundle show -format spiffe > /tmp/aws.bundle   # on AWS
# copy to the GCP server, then:
sudo spire-server bundle set -format spiffe -id spiffe://forge.dev.aws < /tmp/aws.bundle
```

**Verify:** each server lists the peer's trust domain.

```bash
sudo spire-server bundle list
```

Once this lands, `forge-serve` on the AWS side should stop crash-looping within
about 15 seconds and stay up:

```bash
sudo systemctl status forge-serve
curl -sS localhost:8080/healthz
```

Its crash-looping **before** this step is expected, not a fault — the initial
bundle fetch is fatal by design.

---

## Layer 5 — start the agent

The join token is single-use and minted at bootstrap, which is why the agent
ships installed but stopped. On the **GCP** SPIRE server:

```bash
sudo spire-server token generate -spiffeID spiffe://forge.dev.gcp/agent/forge
# -> Token: <value>
sudo forge-agent-join <value>
sudo spire-agent healthcheck -socketPath /tmp/agent.sock
```

---

## Layer 6 — register the workload and prove it

The `-federatesWith` flag is **required**. Without it the SVID carries no
federated audience and the peer rejects it — a failure that looks like a
validation bug rather than a missing flag.

```bash
sudo spire-server entry create \
  -parentID  spiffe://forge.dev.gcp/agent/forge \
  -spiffeID  spiffe://forge.dev.gcp/workload/demo \
  -selector  unix:uid:0 \
  -federatesWith forge.dev.aws
```

Mint an SVID with the **peer** trust domain as audience. The agent needs a sync
cycle before the new entry is usable, so allow a few seconds:

```bash
sudo spire-agent api fetch jwt \
  -audience forge.dev.aws \
  -spiffeID spiffe://forge.dev.gcp/workload/demo \
  -socketPath /tmp/agent.sock
```

Then, from the AWS side:

```bash
curl -sS -X POST localhost:8080/validate -d '{"token":"<jwt>"}'
```

**This is the pass criterion:**

```json
{"valid":true,"spiffe_id":"spiffe://forge.dev.gcp/workload/demo","trust_domain":"forge.dev.gcp"}
```

---

## Tear down

```bash
FORGE_STACK=dev go run ./cmd/forge destroy
```

Then sweep what Pulumi does not track:

```bash
gcloud compute snapshots list --filter="sourceDisk~forge-"   # created by the schedule, not managed
gcloud compute addresses list --filter="status=RESERVED"     # unattached IPs bill double
aws ec2 describe-addresses --query 'Addresses[?AssociationId==null]'
aws ec2 describe-volumes --filters Name=status,Values=available
```

---

## Capture what you typed

Keep the transcript of layers 4–6. Those steps are deliberately unautomated
because their correct shape isn't knowable until they've been run once against
real infrastructure — and that transcript is the specification for automating
them.
