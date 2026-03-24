package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// SPIREOIDCProviderArgs configures the AWS-side OIDC identity provider.
type SPIREOIDCProviderArgs struct {
	Environment         string
	SPIRETrustDomain    string // AWS-side SPIRE trust domain
	GCPSPIRETrustDomain string // GCP-side SPIRE trust domain (for audience)
	EKSClusterName      pulumi.StringOutput
}

// SPIREOIDCProvider configures an IAM OIDC identity provider that accepts
// SPIFFE SVIDs from the GCP SPIRE trust domain, enabling cross-cloud
// workload attestation from GCP to AWS.
type SPIREOIDCProvider struct {
	pulumi.ResourceState

	Arn pulumi.StringOutput
	Url pulumi.StringOutput
}

// NewSPIREOIDCProvider creates the IAM OIDC provider for GCP SPIRE federation.
func NewSPIREOIDCProvider(ctx *pulumi.Context, name string, args *SPIREOIDCProviderArgs, opts ...pulumi.ResourceOption) (*SPIREOIDCProvider, error) {
	component := &SPIREOIDCProvider{}
	err := ctx.RegisterComponentResource("forge:aws:SPIREOIDCProvider", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	// The GCP SPIRE OIDC Discovery endpoint — mirrors how GCP's WorkloadIdentity
	// points at the AWS OIDC endpoint.
	gcpIssuerURL := fmt.Sprintf("https://oidc-discovery.%s", args.GCPSPIRETrustDomain)

	provider, err := iam.NewOpenIdConnectProvider(ctx, namePrefix+"-spire-oidc-gcp", &iam.OpenIdConnectProviderArgs{
		Url: pulumi.String(gcpIssuerURL),
		ClientIdLists: pulumi.StringArray{
			pulumi.Sprintf("spiffe://%s", args.SPIRETrustDomain),
		},
		// Placeholder thumbprint — in production, use the actual TLS certificate thumbprint.
		ThumbprintLists: pulumi.StringArray{
			pulumi.String("0000000000000000000000000000000000000000"),
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-spire-oidc-gcp"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.Arn = provider.Arn
	component.Url = provider.Url

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"arn": provider.Arn,
		"url": provider.Url,
	}); err != nil {
		return nil, err
	}

	return component, nil
}
