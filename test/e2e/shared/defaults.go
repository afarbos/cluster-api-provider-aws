//go:build e2e
// +build e2e

/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shared

import (
	"context"
	"flag"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go/aws"
	"k8s.io/apimachinery/pkg/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"

	infrav1 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	"sigs.k8s.io/cluster-api/test/framework"
)

// Constants.
const (
	DefaultSSHKeyPairName                = "cluster-api-provider-aws-sigs-k8s-io"
	AMIPrefix                            = "capa-ami-ubuntu-24.04-"
	DefaultImageLookupOrg                = "819546954734"
	KubernetesVersion                    = "KUBERNETES_VERSION"
	KubernetesVersionManagement          = "KUBERNETES_VERSION_MANAGEMENT"
	CNIPath                              = "CNI"
	CNIResources                         = "CNI_RESOURCES"
	CNIAddonVersion                      = "VPC_ADDON_VERSION"
	GcWorkloadPath                       = "GC_WORKLOAD"
	KubeproxyAddonVersion                = "KUBE_PROXY_ADDON_VERSION"
	AwsNodeMachineType                   = "AWS_NODE_MACHINE_TYPE"
	AwsAvailabilityZone1                 = "AWS_AVAILABILITY_ZONE_1"
	AwsAvailabilityZone2                 = "AWS_AVAILABILITY_ZONE_2"
	MultiAzFlavor                        = "multi-az"
	LimitAzFlavor                        = "limit-az"
	SpotInstancesFlavor                  = "spot-instances"
	SSMFlavor                            = "ssm"
	TopologyFlavor                       = "topology"
	SelfHostedClusterClassFlavor         = "self-hosted-clusterclass"
	UpgradeToMain                        = "upgrade-to-main"
	ExternalCloudProvider                = "external-cloud-provider"
	SimpleMultitenancyFlavor             = "simple-multitenancy"
	NestedMultitenancyFlavor             = "nested-multitenancy"
	NestedMultitenancyClusterClassFlavor = "nested-multitenancy-clusterclass"
	KCPScaleInFlavor                     = "kcp-scale-in"
	IgnitionFlavor                       = "ignition"
	StorageClassOutTreeZoneLabel         = "topology.ebs.csi.aws.com/zone"
	GPUFlavor                            = "gpu"
	InstanceVcpu                         = "AWS_MACHINE_TYPE_VCPU_USAGE"
	EFSSupport                           = "efs-support"
	IntreeCloudProvider                  = "intree-cloud-provider"
	MultiTenancy                         = "MULTI_TENANCY_"
	EksUpgradeFromVersion                = "UPGRADE_FROM_VERSION"
	EksUpgradeToVersion                  = "UPGRADE_TO_VERSION"

	ClassicElbTestKubernetesFrom = "CLASSICELB_TEST_KUBERNETES_VERSION_FROM"
	ClassicElbTestKubernetesTo   = "CLASSICELB_TEST_KUBERNETES_VERSION_TO"
)

// ResourceQuotaFilePath is the path to the file that contains the resource usage.
var ResourceQuotaFilePath = "/tmp/capa-e2e-resource-usage.lock"

var (
	// MultiTenancySimpleRole is the simple role for multi-tenancy test.
	MultiTenancySimpleRole = MultitenancyRole("Simple")
	// MultiTenancyJumpRole is the jump role for multi-tenancy test.
	MultiTenancyJumpRole = MultitenancyRole("Jump")
	// MultiTenancyNestedRole is the nested role for multi-tenancy test.
	MultiTenancyNestedRole = MultitenancyRole("Nested")

	// MultiTenancyRoles is the list of multi-tenancy roles.
	MultiTenancyRoles = []MultitenancyRole{MultiTenancySimpleRole, MultiTenancyJumpRole, MultiTenancyNestedRole}
	roleLookupCache   = make(map[string]string)
)

// MultitenancyRole is the role of the test.
type MultitenancyRole string

// EnvVarARN returns the environment variable name for the role ARN.
func (m MultitenancyRole) EnvVarARN() string {
	return MultiTenancy + strings.ToUpper(string(m)) + "_ROLE_ARN"
}

// EnvVarName returns the environment variable name for the role name.
func (m MultitenancyRole) EnvVarName() string {
	return MultiTenancy + strings.ToUpper(string(m)) + "_ROLE_NAME"
}

// EnvVarIdentity returns the environment variable name for the identity name.
func (m MultitenancyRole) EnvVarIdentity() string {
	return MultiTenancy + strings.ToUpper(string(m)) + "_IDENTITY_NAME"
}

// IdentityName returns the identity name.
func (m MultitenancyRole) IdentityName() string {
	return strings.ToLower(m.RoleName())
}

// RoleName returns the role name.
func (m MultitenancyRole) RoleName() string {
	return "CAPAMultiTenancy" + string(m)
}

// SetEnvVars sets the environment variables for the role.
func (m MultitenancyRole) SetEnvVars(ctx context.Context, cfg *awsv2.Config) error {
	arn, err := m.RoleARN(ctx, cfg)
	if err != nil {
		return err
	}
	SetEnvVar(m.EnvVarARN(), arn, false)
	SetEnvVar(m.EnvVarName(), m.RoleName(), false)
	SetEnvVar(m.EnvVarIdentity(), m.IdentityName(), false)
	return nil
}

// RoleARN returns the role ARN.
func (m MultitenancyRole) RoleARN(ctx context.Context, cfg *awsv2.Config) (string, error) {
	if roleARN, ok := roleLookupCache[m.RoleName()]; ok {
		return roleARN, nil
	}
	iamSvc := iam.NewFromConfig(*cfg)
	role, err := iamSvc.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(m.RoleName())})
	if err != nil {
		return "", err
	}
	roleARN := *role.Role.Arn
	roleLookupCache[m.RoleName()] = roleARN
	return roleARN, nil
}

// Service codes and quotas can be found under: https://us-west-1.console.aws.amazon.com/servicequotas/home/services
func getLimitedResources() map[string]*ServiceQuota {
	serviceQuotas := map[string]*ServiceQuota{}
	serviceQuotas["igw"] = &ServiceQuota{
		ServiceCode:         "vpc",
		QuotaName:           "Internet gateways per Region",
		QuotaCode:           "L-A4707A72",
		DesiredMinimumValue: 20,
	}

	serviceQuotas["ngw"] = &ServiceQuota{
		ServiceCode:         "vpc",
		QuotaName:           "NAT gateways per Availability Zone",
		QuotaCode:           "L-FE5A380F",
		DesiredMinimumValue: 20,
	}

	serviceQuotas["vpc"] = &ServiceQuota{
		ServiceCode:         "vpc",
		QuotaName:           "VPCs per Region",
		QuotaCode:           "L-F678F1CE",
		DesiredMinimumValue: 25,
	}

	serviceQuotas["ec2-normal"] = &ServiceQuota{
		ServiceCode:         "ec2",
		QuotaName:           "Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances",
		QuotaCode:           "L-1216C47A",
		DesiredMinimumValue: 128,
	}

	serviceQuotas["eip"] = &ServiceQuota{
		ServiceCode:         "ec2",
		QuotaName:           "EC2-VPC Elastic IPs",
		QuotaCode:           "L-0263D0A3",
		DesiredMinimumValue: 100,
	}

	serviceQuotas["classiclb"] = &ServiceQuota{
		ServiceCode:         "elasticloadbalancing",
		QuotaName:           "Classic Load Balancers per Region",
		QuotaCode:           "L-E9E9831D",
		DesiredMinimumValue: 20,
	}

	serviceQuotas["ec2-GPU"] = &ServiceQuota{
		ServiceCode:         "ec2",
		QuotaName:           "Running On-Demand G and VT instances",
		QuotaCode:           "L-DB2E81BA",
		DesiredMinimumValue: 8,
	}

	serviceQuotas["volume-GP2"] = &ServiceQuota{
		ServiceCode:         "ebs",
		QuotaName:           "Storage for General Purpose SSD (gp2) volumes, in TiB",
		QuotaCode:           "L-D18FCD1D",
		DesiredMinimumValue: 50,
	}

	serviceQuotas["eventBridge-rules"] = &ServiceQuota{
		ServiceCode:         "events",
		QuotaName:           "Maximum number of rules an account can have per event bus",
		QuotaCode:           "L-244521F2",
		DesiredMinimumValue: 500,
	}

	return serviceQuotas
}

// DefaultScheme returns the default scheme to use for testing.
func DefaultScheme() *runtime.Scheme {
	sc := runtime.NewScheme()
	framework.TryAddDefaultSchemes(sc)
	_ = infrav1.AddToScheme(sc)
	_ = cgscheme.AddToScheme(sc)
	return sc
}

// CreateDefaultFlags will create the default flags used for the tests and binds them to the e2e context.
func CreateDefaultFlags(ctx *E2EContext) {
	flag.StringVar(&ctx.Settings.ConfigPath, "config-path", "", "path to the e2e config file")
	flag.StringVar(&ctx.Settings.ArtifactFolder, "artifacts-folder", "", "folder where e2e test artifact should be stored")
	flag.BoolVar(&ctx.Settings.UseCIArtifacts, "kubetest.use-ci-artifacts", false, "use the latest build from the main branch of the Kubernetes repository")
	flag.StringVar(&ctx.Settings.KubetestConfigFilePath, "kubetest.config-file", "", "path to the kubetest configuration file")
	flag.IntVar(&ctx.Settings.GinkgoNodes, "kubetest.ginkgo-nodes", 1, "number of ginkgo nodes to use")
	flag.IntVar(&ctx.Settings.GinkgoSlowSpecThreshold, "kubetest.ginkgo-slowSpecThreshold", 120, "time in s before spec is marked as slow")
	flag.BoolVar(&ctx.Settings.UseExistingCluster, "use-existing-cluster", false, "if true, the test uses the current cluster instead of creating a new one (default discovery rules apply)")
	flag.BoolVar(&ctx.Settings.SkipCleanup, "skip-cleanup", false, "if true, the resource cleanup after tests will be skipped")
	flag.BoolVar(&ctx.Settings.SkipCloudFormationDeletion, "skip-cloudformation-deletion", false, "if true, an AWS CloudFormation stack will not be deleted")
	flag.BoolVar(&ctx.Settings.SkipCloudFormationCreation, "skip-cloudformation-creation", false, "if true, an AWS CloudFormation stack will not be created")
	flag.BoolVar(&ctx.Settings.SkipQuotas, "skip-quotas", false, "if true, the requesting of quotas for aws services will be skipped")
	flag.StringVar(&ctx.Settings.DataFolder, "data-folder", "", "path to the data folder")
	flag.StringVar(&ctx.Settings.SourceTemplate, "source-template", "infrastructure-aws/withoutclusterclass/generated/cluster-template.yaml", "path to the data folder")
}
