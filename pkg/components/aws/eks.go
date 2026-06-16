package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/eks"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// EKSClusterArgs configures the EKS cluster component.
type EKSClusterArgs struct {
	Environment  string
	VpcID        pulumi.IDOutput
	SubnetIDs    pulumi.StringArrayOutput
	NodeCount    int
	InstanceType string
}

// EKSCluster is a component resource for an EKS cluster with
// IRSA and SPIRE-compatible configuration.
type EKSCluster struct {
	pulumi.ResourceState

	Name     pulumi.StringOutput
	Endpoint pulumi.StringOutput
}

// NewEKSCluster provisions an EKS cluster with a managed node group.
func NewEKSCluster(ctx *pulumi.Context, name string, args *EKSClusterArgs, opts ...pulumi.ResourceOption) (*EKSCluster, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	component := &EKSCluster{}
	err := ctx.RegisterComponentResource("forge:aws:EKSCluster", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	// IAM role for EKS cluster
	clusterRole, err := iam.NewRole(ctx, namePrefix+"-eks-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"Service": "eks.amazonaws.com"},
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-eks-role")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = iam.NewRolePolicyAttachment(ctx, namePrefix+"-eks-policy", &iam.RolePolicyAttachmentArgs{
		Role:      clusterRole.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// EKS cluster
	cluster, err := eks.NewCluster(ctx, namePrefix+"-eks", &eks.ClusterArgs{
		RoleArn: clusterRole.Arn,
		VpcConfig: &eks.ClusterVpcConfigArgs{
			SubnetIds:             args.SubnetIDs,
			EndpointPrivateAccess: pulumi.Bool(true),
			EndpointPublicAccess:  pulumi.Bool(true),
		},
		EnabledClusterLogTypes: pulumi.StringArray{
			pulumi.String("api"),
			pulumi.String("audit"),
			pulumi.String("authenticator"),
		},
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-eks")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// IAM role for node group
	nodeRole, err := iam.NewRole(ctx, namePrefix+"-node-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"Service": "ec2.amazonaws.com"},
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-node-role")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	for _, policy := range []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
	} {
		_, err = iam.NewRolePolicyAttachment(ctx, namePrefix+"-node-"+policy[len(policy)-10:], &iam.RolePolicyAttachmentArgs{
			Role:      nodeRole.Name,
			PolicyArn: pulumi.String(policy),
		}, parentOpt)
		if err != nil {
			return nil, err
		}
	}

	// Managed node group
	_, err = eks.NewNodeGroup(ctx, namePrefix+"-nodegroup", &eks.NodeGroupArgs{
		ClusterName: cluster.Name,
		NodeRoleArn: nodeRole.Arn,
		SubnetIds:   args.SubnetIDs,
		ScalingConfig: &eks.NodeGroupScalingConfigArgs{
			DesiredSize: pulumi.Int(args.NodeCount),
			MinSize:     pulumi.Int(args.NodeCount),
			MaxSize:     pulumi.Int(args.NodeCount),
		},
		InstanceTypes: pulumi.StringArray{pulumi.String(args.InstanceType)},
		Tags:          pulumi.StringMap{"Name": pulumi.String(namePrefix + "-nodegroup")},
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
