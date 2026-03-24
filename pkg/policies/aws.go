package policies

// AWSVPCPolicyInput captures security-relevant AWS VPC configuration.
type AWSVPCPolicyInput struct {
	Environment    string
	ResourceName   string
	CustomVPC      bool
	MultiAZ        bool
	PrivateSubnets bool
}

// EKSPolicyInput captures security-relevant EKS configuration.
type EKSPolicyInput struct {
	Environment      string
	ResourceName     string
	PrivateEndpoint  bool
	EncryptedSecrets bool
	LoggingEnabled   bool
}

// SPIREOIDCPolicyInput captures AWS OIDC provider configuration.
type SPIREOIDCPolicyInput struct {
	Environment    string
	ResourceName   string
	OIDCIssuerSet  bool
	TrustDomainSet bool
}

// CheckAWSVPC validates AWS VPC security policies.
func CheckAWSVPC(input AWSVPCPolicyInput, r *Result) {
	if !input.CustomVPC {
		r.Add("aws-vpc-custom", Mandatory,
			"AWS VPC must be a custom VPC (not the default VPC)")
	}
	if !input.MultiAZ {
		r.Add("aws-vpc-multi-az", Mandatory,
			"AWS VPC must have subnets in multiple availability zones")
	}
	if !input.PrivateSubnets {
		r.Add("aws-vpc-private-subnets", Mandatory,
			"AWS VPC subnets must not map public IPs")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}

// CheckEKS validates EKS cluster security policies.
func CheckEKS(input EKSPolicyInput, r *Result) {
	if !input.PrivateEndpoint {
		r.Add("eks-private-endpoint", Mandatory,
			"EKS cluster must enable private endpoint access")
	}
	if !input.EncryptedSecrets {
		r.Add("eks-encrypted-secrets", Mandatory,
			"EKS cluster must encrypt secrets at rest")
	}
	if !input.LoggingEnabled {
		r.Add("eks-logging", Advisory,
			"EKS cluster should enable control plane logging")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}

// CheckSPIREOIDC validates AWS SPIRE OIDC provider policies.
func CheckSPIREOIDC(input SPIREOIDCPolicyInput, r *Result) {
	if !input.OIDCIssuerSet {
		r.Add("spire-oidc-issuer", Mandatory,
			"SPIRE OIDC provider must have an issuer URL configured")
	}
	if !input.TrustDomainSet {
		r.Add("spire-oidc-trust-domain", Mandatory,
			"SPIRE OIDC provider must have a trust domain audience configured")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}
