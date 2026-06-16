package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// WorkloadIdentityArgs configures cross-cloud workload identity federation.
type WorkloadIdentityArgs struct {
	Environment       string
	SPIRETrustDomain  string // GCP-side SPIRE trust domain
	AWSSPIRETrustDomain string // AWS-side SPIRE trust domain for federation
	GKEClusterName    pulumi.StringOutput
}

// WorkloadIdentity is a component resource that configures GCP Workload
// Identity Federation to accept SPIFFE SVIDs from an external (AWS) SPIRE
// trust domain, enabling cross-cloud workload attestation.
type WorkloadIdentity struct {
	pulumi.ResourceState

	PoolID     pulumi.StringOutput
	ProviderID pulumi.StringOutput
}

// NewWorkloadIdentity creates the WIF pool and OIDC provider for SPIFFE federation.
func NewWorkloadIdentity(ctx *pulumi.Context, name string, args *WorkloadIdentityArgs, opts ...pulumi.ResourceOption) (*WorkloadIdentity, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	component := &WorkloadIdentity{}
	err := ctx.RegisterComponentResource("forge:gcp:WorkloadIdentity", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	// Workload Identity Pool — acts as the trust boundary for external identities
	pool, err := iam.NewWorkloadIdentityPool(ctx, namePrefix+"-spiffe-pool", &iam.WorkloadIdentityPoolArgs{
		WorkloadIdentityPoolId: pulumi.String(namePrefix + "-spiffe"),
		DisplayName:            pulumi.Sprintf("Forge %s SPIFFE Federation", args.Environment),
		Description:            pulumi.String("Accepts SPIFFE SVIDs from federated SPIRE trust domains"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// OIDC Provider — configured to accept JWTs (SPIFFE SVIDs) from the AWS SPIRE server.
	// The SPIRE OIDC Discovery endpoint serves the JWKS for token validation.
	//
	// In production, the issuer URI points to the SPIRE OIDC Discovery Provider
	// running in the AWS cluster (e.g., https://oidc-discovery.forge.dev.aws.example.com).
	awsIssuerURI := fmt.Sprintf("https://oidc-discovery.%s", args.AWSSPIRETrustDomain)

	provider, err := iam.NewWorkloadIdentityPoolProvider(ctx, namePrefix+"-spiffe-aws", &iam.WorkloadIdentityPoolProviderArgs{
		WorkloadIdentityPoolId:         pool.WorkloadIdentityPoolId,
		WorkloadIdentityPoolProviderId: pulumi.String(namePrefix + "-spiffe-aws"),
		DisplayName:                    pulumi.Sprintf("AWS SPIRE Trust Domain (%s)", args.AWSSPIRETrustDomain),

		// OIDC federation with the AWS SPIRE OIDC Discovery Provider
		Oidc: &iam.WorkloadIdentityPoolProviderOidcArgs{
			IssuerUri: pulumi.String(awsIssuerURI),
			AllowedAudiences: pulumi.StringArray{
				// The audience the SPIRE agent includes in minted SVIDs
				pulumi.Sprintf("spiffe://%s", args.SPIRETrustDomain),
			},
		},

		// Attribute mapping — map SPIFFE ID claims to Google attributes
		// for fine-grained IAM binding
		AttributeMapping: pulumi.StringMap{
			"google.subject":         pulumi.String("assertion.sub"), // SPIFFE ID
			"attribute.trust_domain": pulumi.String("assertion.sub.extract('spiffe://{trust_domain}/')"),
			"attribute.workload":     pulumi.String("assertion.sub.extract('/{workload_id}')"),
		},

		// Attribute condition — only accept SVIDs from the expected trust domain
		AttributeCondition: pulumi.Sprintf(
			"assertion.sub.startsWith('spiffe://%s/')",
			args.AWSSPIRETrustDomain,
		),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.PoolID = pool.WorkloadIdentityPoolId
	component.ProviderID = provider.WorkloadIdentityPoolProviderId

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"poolId":     pool.WorkloadIdentityPoolId,
		"providerId": provider.WorkloadIdentityPoolProviderId,
	}); err != nil {
		return nil, err
	}

	return component, nil
}
