package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/wingnut128/forge/pkg/spire"
)

// SPIREServerArgs configures the GCP SPIRE server VM.
type SPIREServerArgs struct {
	Environment      string
	Region           string
	Zone             string
	MgmtSubnetLink   pulumi.StringOutput
	VPCID            pulumi.IDOutput
	MachineType      string // default: e2-small
	SPIREVersion     string
	TrustDomain      string
	PeerTrustDomain  string
	ManagedStateMode bool
}

// SPIREServer provisions a single GCE VM + persistent disk to host spire-server.
// Daily disk snapshots preserve state across instance replacements.
type SPIREServer struct {
	pulumi.ResourceState

	InstanceName pulumi.StringOutput
	InternalIP   pulumi.StringOutput
}

// NewSPIREServer provisions the VM, data disk, snapshot schedule, and firewall.
func NewSPIREServer(ctx *pulumi.Context, name string, args *SPIREServerArgs, opts ...pulumi.ResourceOption) (*SPIREServer, error) {
	component := &SPIREServer{}
	if err := ctx.RegisterComponentResource("forge:gcp:SPIREServer", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	machineType := args.MachineType
	if machineType == "" {
		machineType = "e2-small"
	}
	zone := args.Zone
	if zone == "" {
		zone = args.Region + "-a"
	}

	// Snapshot schedule so state is recoverable even after disk loss.
	snapPolicy, err := compute.NewResourcePolicy(ctx, namePrefix+"-spire-snap", &compute.ResourcePolicyArgs{
		Region: pulumi.String(args.Region),
		SnapshotSchedulePolicy: &compute.ResourcePolicySnapshotSchedulePolicyArgs{
			Schedule: &compute.ResourcePolicySnapshotSchedulePolicyScheduleArgs{
				DailySchedule: &compute.ResourcePolicySnapshotSchedulePolicyScheduleDailyScheduleArgs{
					DaysInCycle: pulumi.Int(1),
					StartTime:   pulumi.String("03:00"),
				},
			},
			RetentionPolicy: &compute.ResourcePolicySnapshotSchedulePolicyRetentionPolicyArgs{
				MaxRetentionDays: pulumi.Int(7),
			},
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	dataDisk, err := compute.NewDisk(ctx, namePrefix+"-spire-data", &compute.DiskArgs{
		Zone:            pulumi.String(zone),
		Size:            pulumi.Int(20),
		Type:            pulumi.String("pd-standard"),
		ResourcePolicies: pulumi.StringArray{snapPolicy.ID().ToStringOutput()},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	startupScript := spireGCPStartupScript(args)

	instance, err := compute.NewInstance(ctx, namePrefix+"-spire-server", &compute.InstanceArgs{
		MachineType: pulumi.String(machineType),
		Zone:        pulumi.String(zone),
		BootDisk: &compute.InstanceBootDiskArgs{
			InitializeParams: &compute.InstanceBootDiskInitializeParamsArgs{
				Image: pulumi.String("debian-cloud/debian-12"),
				Size:  pulumi.Int(10),
			},
		},
		AttachedDisks: compute.InstanceAttachedDiskArray{
			&compute.InstanceAttachedDiskArgs{
				Source:     dataDisk.SelfLink,
				DeviceName: pulumi.String("spire-data"),
			},
		},
		NetworkInterfaces: compute.InstanceNetworkInterfaceArray{
			&compute.InstanceNetworkInterfaceArgs{
				Subnetwork: args.MgmtSubnetLink,
				// No access_config -> no public IP. Egress via Cloud NAT.
			},
		},
		MetadataStartupScript: pulumi.String(startupScript),
		ShieldedInstanceConfig: &compute.InstanceShieldedInstanceConfigArgs{
			EnableSecureBoot:          pulumi.Bool(true),
			EnableIntegrityMonitoring: pulumi.Bool(true),
		},
		Tags: pulumi.StringArray{pulumi.String("spire-server")},
		Labels: pulumi.StringMap{
			"component":   pulumi.String("spire-server"),
			"environment": pulumi.String(args.Environment),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Firewall: allow SPIRE server bundle endpoint + gRPC from mgmt subnet peers.
	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-spire", &compute.FirewallArgs{
		Network: args.VPCID.ToStringOutput(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports:    pulumi.StringArray{pulumi.String("8443"), pulumi.String("8081")},
			},
		},
		SourceRanges: pulumi.StringArray{pulumi.String("10.0.0.0/8")},
		TargetTags:   pulumi.StringArray{pulumi.String("spire-server")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.InstanceName = instance.Name
	component.InternalIP = instance.NetworkInterfaces.Index(pulumi.Int(0)).NetworkIp().Elem()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceName": instance.Name,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func spireGCPStartupScript(args *SPIREServerArgs) string {
	mode := spire.StateModeDisk
	if args.ManagedStateMode {
		mode = spire.StateModeManaged
	}
	// Phase 2 wires the real peer bundle endpoint (peer SPIRE server IP).
	// For now derive a placeholder so config renders; live VMs are not yet
	// exercised end-to-end (see specs/2026-06-15-spire-bootstrap-local-proof).
	serverHCL, err := spire.RenderServerHCL(spire.ServerConfig{
		TrustDomain:           args.TrustDomain,
		PeerTrustDomain:       args.PeerTrustDomain,
		PeerBundleEndpointURL: fmt.Sprintf("https://%s:8443", args.PeerTrustDomain),
		StateMode:             mode,
		ManagedDBConnString:   "postgres://spire@127.0.0.1:5432/spire", // Phase 2: real managed DSN
	})
	if err != nil {
		// Render only fails on missing required fields; surface as a config error
		// baked into the script so the VM logs make the cause obvious.
		serverHCL = "# ERROR rendering SPIRE config: " + err.Error()
	}

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
SPIRE_VERSION=%q

mkdir -p /var/lib/spire
if ! mountpoint -q /var/lib/spire; then
  DEV=/dev/disk/by-id/google-spire-data
  if ! blkid "$DEV" >/dev/null 2>&1; then
    mkfs.ext4 -F "$DEV"
  fi
  echo "$DEV /var/lib/spire ext4 defaults,nofail 0 2" >> /etc/fstab
  mount /var/lib/spire
fi

if [ ! -x /usr/local/bin/spire-server ]; then
  cd /tmp
  curl -sSL -o spire.tar.gz "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz"
  tar -xzf spire.tar.gz
  install -m 0755 spire-${SPIRE_VERSION}/bin/spire-server /usr/local/bin/spire-server
fi

mkdir -p /etc/spire /etc/spire/certs /var/lib/spire/data
cat >/etc/spire/server.conf <<'CONF'
%s
CONF

cat >/etc/systemd/system/spire-server.service <<UNIT
[Unit]
Description=SPIRE Server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/spire-server run -config /etc/spire/server.conf
Restart=always

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now spire-server
`, args.SPIREVersion, serverHCL)
}
