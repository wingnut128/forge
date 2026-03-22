package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NetworkArgs configures the VPC network component.
type NetworkArgs struct {
	Environment string
}

// Network is a Pulumi component resource that provisions a VPC with
// a primary subnet and secondary ranges for GKE pods/services.
type Network struct {
	pulumi.ResourceState

	ID       pulumi.IDOutput
	SubnetID pulumi.IDOutput
}

// NewNetwork creates the VPC, subnet, and firewall rules.
func NewNetwork(ctx *pulumi.Context, name string, args *NetworkArgs, opts ...pulumi.ResourceOption) (*Network, error) {
	component := &Network{}
	err := ctx.RegisterComponentResource("forge:gcp:Network", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	// VPC — custom subnet mode for explicit control
	vpc, err := compute.NewNetwork(ctx, namePrefix+"-vpc", &compute.NetworkArgs{
		AutoCreateSubnetworks: pulumi.Bool(false),
		Description:           pulumi.Sprintf("Forge %s VPC", args.Environment),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Primary subnet with secondary ranges for GKE
	subnet, err := compute.NewSubnetwork(ctx, namePrefix+"-subnet", &compute.SubnetworkArgs{
		Network:     vpc.ID(),
		IpCidrRange: pulumi.String("10.0.0.0/20"),
		Region:      pulumi.String("us-central1"),
		SecondaryIpRanges: compute.SubnetworkSecondaryIpRangeArray{
			&compute.SubnetworkSecondaryIpRangeArgs{
				RangeName:   pulumi.String("pods"),
				IpCidrRange: pulumi.String("10.4.0.0/14"),
			},
			&compute.SubnetworkSecondaryIpRangeArgs{
				RangeName:   pulumi.String("services"),
				IpCidrRange: pulumi.String("10.8.0.0/20"),
			},
		},
		PrivateIpGoogleAccess: pulumi.Bool(true),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Allow internal traffic within the VPC
	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-internal", &compute.FirewallArgs{
		Network: vpc.ID(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports:    pulumi.StringArray{pulumi.String("0-65535")},
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("udp"),
				Ports:    pulumi.StringArray{pulumi.String("0-65535")},
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("icmp"),
			},
		},
		SourceRanges: pulumi.StringArray{pulumi.String("10.0.0.0/8")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.ID = vpc.ID()
	component.SubnetID = subnet.ID()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":    vpc.ID(),
		"subnetId": subnet.ID(),
	}); err != nil {
		return nil, err
	}

	return component, nil
}
