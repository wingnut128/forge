package policies

import "strings"

// GKEPolicyInput captures security-relevant GKE configuration for policy validation.
// Fields correspond to hardcoded values in NewGKECluster, extracted so policies
// can be tested without Pulumi dependencies.
type GKEPolicyInput struct {
	Environment       string
	ResourceName      string
	PrivateCluster    bool
	WorkloadIdentity  bool
	BinaryAuthEnabled bool
	NetworkPolicy     bool
	SecureBoot        bool
	IntegrityMonitor  bool
	AutoRepair        bool
	AutoUpgrade       bool
}

// NetworkPolicyInput captures network configuration for policy validation.
type NetworkPolicyInput struct {
	Environment         string
	ResourceName        string
	CustomSubnetMode    bool
	PrivateGoogleAccess bool
}

// WorkloadIdentityPolicyInput captures WIF configuration for policy validation.
type WorkloadIdentityPolicyInput struct {
	Environment        string
	ResourceName       string
	AttributeCondition string
	HasAudiences       bool
}

// CheckGKE validates GKE cluster security policies.
func CheckGKE(input GKEPolicyInput, r *Result) {
	if !input.PrivateCluster {
		r.Add("gke-private-cluster", Mandatory,
			"GKE cluster must use private nodes (EnablePrivateNodes = true)")
	}
	if !input.WorkloadIdentity {
		r.Add("gke-workload-identity", Mandatory,
			"GKE cluster must have Workload Identity enabled")
	}
	if !input.BinaryAuthEnabled {
		r.Add("gke-binary-authorization", Mandatory,
			"GKE cluster must enforce binary authorization")
	}
	if !input.NetworkPolicy {
		r.Add("gke-network-policy", Mandatory,
			"GKE cluster must enable network policy")
	}
	if !input.SecureBoot {
		r.Add("gke-shielded-secure-boot", Mandatory,
			"GKE node pools must enable Secure Boot")
	}
	if !input.IntegrityMonitor {
		r.Add("gke-shielded-integrity", Mandatory,
			"GKE node pools must enable Integrity Monitoring")
	}
	if !input.AutoRepair {
		r.Add("gke-auto-repair", Advisory,
			"GKE node pools should enable auto-repair")
	}
	if !input.AutoUpgrade {
		r.Add("gke-auto-upgrade", Advisory,
			"GKE node pools should enable auto-upgrade")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}

// CheckNetwork validates VPC network security policies.
func CheckNetwork(input NetworkPolicyInput, r *Result) {
	if !input.CustomSubnetMode {
		r.Add("network-custom-subnet", Mandatory,
			"VPC must use custom subnet mode (AutoCreateSubnetworks = false)")
	}
	if !input.PrivateGoogleAccess {
		r.Add("network-private-google-access", Mandatory,
			"Subnets must enable Private Google Access")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}

// CheckWorkloadIdentity validates WIF security policies.
func CheckWorkloadIdentity(input WorkloadIdentityPolicyInput, r *Result) {
	if input.AttributeCondition == "" {
		r.Add("wif-attribute-condition", Mandatory,
			"Workload Identity provider must set an attribute condition")
	}
	if !input.HasAudiences {
		r.Add("wif-allowed-audiences", Mandatory,
			"Workload Identity OIDC provider must have allowed audiences")
	}
	checkNaming(input.ResourceName, input.Environment, r)
}

// checkNaming verifies the forge-{env}-* naming convention.
func checkNaming(resourceName, environment string, r *Result) {
	prefix := "forge-" + environment + "-"
	if !strings.HasPrefix(resourceName, prefix) {
		r.Add("naming-convention", Mandatory,
			"resource name %q must start with %q", resourceName, prefix)
	}
}
