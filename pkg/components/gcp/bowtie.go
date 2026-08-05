package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// BowtieControllerArgs configures the GCP Bowtie controller VM.
type BowtieControllerArgs struct {
	Environment    string
	Region         string
	Zone           string
	MgmtSubnetLink pulumi.StringOutput
	VPCID          pulumi.IDOutput
	Image          string // e.g. projects/bowtie-works/global/images/bowtie-controller-gce-efi-<version>
	MachineType    string // default: e2-small
	AdminCIDRs     []string
}

// BowtieController provisions a single Bowtie controller VM with a reserved
// external IP so it's reachable by clients and the peer CSP controller.
type BowtieController struct {
	pulumi.ResourceState

	InstanceName pulumi.StringOutput
	ExternalIP   pulumi.StringOutput
	InternalIP   pulumi.StringOutput
}

// NewBowtieController provisions the VM, static external IP, and admin firewall.
func NewBowtieController(ctx *pulumi.Context, name string, args *BowtieControllerArgs, opts ...pulumi.ResourceOption) (*BowtieController, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	if args.Image == "" {
		return nil, fmt.Errorf("bowtie-gcp-image config is required when Bowtie is enabled")
	}
	component := &BowtieController{}
	if err := ctx.RegisterComponentResource("forge:gcp:BowtieController", name, component, opts...); err != nil {
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

	extIP, err := compute.NewAddress(ctx, namePrefix+"-bowtie-ip", &compute.AddressArgs{
		Region: pulumi.String(args.Region),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	instance, err := compute.NewInstance(ctx, namePrefix+"-bowtie", &compute.InstanceArgs{
		MachineType: pulumi.String(machineType),
		Zone:        pulumi.String(zone),
		BootDisk: &compute.InstanceBootDiskArgs{
			InitializeParams: &compute.InstanceBootDiskInitializeParamsArgs{
				Image: pulumi.String(args.Image),
				Size:  pulumi.Int(20),
			},
		},
		NetworkInterfaces: compute.InstanceNetworkInterfaceArray{
			&compute.InstanceNetworkInterfaceArgs{
				Subnetwork: args.MgmtSubnetLink,
				AccessConfigs: compute.InstanceNetworkInterfaceAccessConfigArray{
					&compute.InstanceNetworkInterfaceAccessConfigArgs{
						NatIp: extIP.Address,
					},
				},
			},
		},
		Tags: pulumi.StringArray{pulumi.String("bowtie-controller")},
		Labels: pulumi.StringMap{
			"component":   pulumi.String("bowtie-controller"),
			"environment": pulumi.String(args.Environment),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	adminCIDRs := args.AdminCIDRs
	if len(adminCIDRs) == 0 {
		// Fail-closed default: no public admin until operator sets the allowlist.
		adminCIDRs = []string{"127.0.0.1/32"}
	}
	cidrs := pulumi.StringArray{}
	for _, c := range adminCIDRs {
		cidrs = append(cidrs, pulumi.String(c))
	}

	// Admin UI / SSH restricted to allowlist.
	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-bowtie-admin", &compute.FirewallArgs{
		Network: args.VPCID.ToStringOutput(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports:    pulumi.StringArray{pulumi.String("22"), pulumi.String("443")},
			},
		},
		SourceRanges: cidrs,
		TargetTags:   pulumi.StringArray{pulumi.String("bowtie-controller")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Bowtie mesh (WireGuard) on udp/51820 — open to any peer; authentication
	// is enforced by Bowtie's own key exchange.
	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-bowtie-mesh", &compute.FirewallArgs{
		Network: args.VPCID.ToStringOutput(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("udp"),
				Ports:    pulumi.StringArray{pulumi.String("51820")},
			},
		},
		SourceRanges: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
		TargetTags:   pulumi.StringArray{pulumi.String("bowtie-controller")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.InstanceName = instance.Name
	component.ExternalIP = extIP.Address
	component.InternalIP = instance.NetworkInterfaces.Index(pulumi.Int(0)).NetworkIp().Elem()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceName": instance.Name,
		"externalIp":   extIP.Address,
	}); err != nil {
		return nil, err
	}
	return component, nil
}
