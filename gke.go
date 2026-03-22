package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/container"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// GKEClusterArgs configures the GKE cluster component.
type GKEClusterArgs struct {
	Environment string
	NetworkID   pulumi.IDOutput
	SubnetID    pulumi.IDOutput
	NodeCount   int
	MachineType string
}

// GKECluster is a component resource for a GKE cluster with
// Workload Identity and SPIRE-compatible configuration.
type GKECluster struct {
	pulumi.ResourceState

	Name       pulumi.StringOutput
	Endpoint   pulumi.StringOutput
	KubeConfig pulumi.StringOutput
}

// NewGKECluster provisions a private GKE cluster with Workload Identity enabled.
func NewGKECluster(ctx *pulumi.Context, name string, args *GKEClusterArgs, opts ...pulumi.ResourceOption) (*GKECluster, error) {
	component := &GKECluster{}
	err := ctx.RegisterComponentResource("forge:gcp:GKECluster", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	cluster, err := container.NewCluster(ctx, namePrefix+"-gke", &container.ClusterArgs{
		Network:    args.NetworkID.ToStringOutput(),
		Subnetwork: args.SubnetID.ToStringOutput(),

		// Use separately managed node pool
		RemoveDefaultNodePool: pulumi.Bool(true),
		InitialNodeCount:      pulumi.Int(1),

		// Workload Identity — required for SPIRE GCP attestation
		WorkloadIdentityConfig: &container.ClusterWorkloadIdentityConfigArgs{
			WorkloadPool: pulumi.Sprintf("%s.svc.id.goog", ctx.Project()),
		},

		// Private cluster — nodes have no public IPs
		PrivateClusterConfig: &container.ClusterPrivateClusterConfigArgs{
			EnablePrivateNodes:    pulumi.Bool(true),
			EnablePrivateEndpoint: pulumi.Bool(false), // allow kubectl from outside
			MasterIpv4CidrBlock:  pulumi.String("172.16.0.0/28"),
		},

		// Binary authorization for supply chain security
		BinaryAuthorization: &container.ClusterBinaryAuthorizationArgs{
			EvaluationMode: pulumi.String("PROJECT_SINGLETON_POLICY_ENFORCE"),
		},

		IpAllocationPolicy: &container.ClusterIpAllocationPolicyArgs{
			ClusterSecondaryRangeName:  pulumi.String("pods"),
			ServicesSecondaryRangeName: pulumi.String("services"),
		},

		// Enable network policy for SPIRE agent communication
		NetworkPolicy: &container.ClusterNetworkPolicyArgs{
			Enabled:  pulumi.Bool(true),
			Provider: pulumi.String("CALICO"),
		},

		ReleaseChannel: &container.ClusterReleaseChannelArgs{
			Channel: pulumi.String("REGULAR"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Dedicated node pool
	_, err = container.NewNodePool(ctx, namePrefix+"-nodepool", &container.NodePoolArgs{
		Cluster:   cluster.Name,
		NodeCount: pulumi.Int(args.NodeCount),
		NodeConfig: &container.NodePoolNodeConfigArgs{
			MachineType: pulumi.String(args.MachineType),
			OauthScopes: pulumi.StringArray{
				pulumi.String("https://www.googleapis.com/auth/cloud-platform"),
			},
			// Workload Identity on nodes
			WorkloadMetadataConfig: &container.NodePoolNodeConfigWorkloadMetadataConfigArgs{
				Mode: pulumi.String("GKE_METADATA"),
			},
			ShieldedInstanceConfig: &container.NodePoolNodeConfigShieldedInstanceConfigArgs{
				EnableSecureBoot:          pulumi.Bool(true),
				EnableIntegrityMonitoring: pulumi.Bool(true),
			},
		},
		Management: &container.NodePoolManagementArgs{
			AutoRepair:  pulumi.Bool(true),
			AutoUpgrade: pulumi.Bool(true),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.Name = cluster.Name
	component.Endpoint = cluster.Endpoint

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"clusterName": cluster.Name,
		"endpoint":    cluster.Endpoint,
	}); err != nil {
		return nil, err
	}

	return component, nil
}
