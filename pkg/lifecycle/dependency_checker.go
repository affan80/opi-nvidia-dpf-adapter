/*
Copyright 2026 Kartikey Gupta.

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

package lifecycle

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SupportedKubernetesVersion is the cluster minor this adapter is tested on
// (config/versions.yaml). Patch and +k3s build metadata are ignored: v1.36.4
// and v1.36.4+k3s1 are the same claim.
const SupportedKubernetesVersion = "v1.36.4"

// DPF Helm prerequisites for the pinned release. Names match the NVIDIA
// helm-prerequisites table. Kamaji is conditional: required only when a
// DPUCluster asks for type=kamaji.
const (
	DepCertManager = "cert-manager"
	DepArgoCD      = "argo-cd"
	DepKamaji      = "kamaji"
	// DepCluster is reported when discovery or the probe is missing.
	DepCluster = "cluster"
)

const clusterAccessMsg = "cannot reach the Kubernetes API"

const (
	certManagerGroup = "cert-manager.io"
	certManagerKind  = "Certificate"
	certManagerVer   = "v1"

	argoGroup = "argoproj.io"
	argoKind  = "Application"
	argoVer   = "v1alpha1"

	kamajiGroup = "kamaji.clastix.io"
	kamajiKind  = "TenantControlPlane"
	kamajiVer   = "v1alpha1"
)

var dpuClusterListGVK = schema.GroupVersionKind{
	Group:   nvidiaProbeGVK.Group,
	Version: nvidiaProbeGVK.Version,
	Kind:    "DPUClusterList",
}

// ClusterProbe is the cluster view the dependency checker reads.
// Tests inject a StaticProbe so they do not need a live apiserver.
type ClusterProbe interface {
	ServerVersion(ctx context.Context) (string, error)
	HasAPI(group, kind, version string) bool
	NeedsKamaji(ctx context.Context) bool
	// Deployment is the post-install health of one controller. A missing
	// name is Present=false (Installing), not an error.
	Deployment(ctx context.Context, namespace, name string) ResourceCheck
}

// StaticProbe is a ClusterProbe for tests. It never talks to a cluster.
type StaticProbe struct {
	Version     string
	VersionErr  error
	Present     map[string]bool
	KamajiNeed  bool
	Deployments map[string]ResourceCheck
}

// ReadyClusterProbe is a passing NVIDIA cluster: supported Kubernetes plus
// the required DPF v26.4 Helm prerequisites. Kamaji is not required.
func ReadyClusterProbe() ClusterProbe {
	present := map[string]bool{
		apiKey(certManagerGroup, certManagerKind): true,
		apiKey(argoGroup, argoKind):               true,
	}
	for _, crd := range requiredDPFCRDs {
		present[apiKey(crd.Group, crd.Kind)] = true
	}
	deploys := map[string]ResourceCheck{}
	for _, d := range append(requiredDPFControllers, requiredPrereqControllers...) {
		deploys[releaseKey(d.Namespace, d.Name)] = ResourceCheck{
			Name: d.Name, Present: true, Healthy: true,
		}
	}
	return StaticProbe{
		Version:     SupportedKubernetesVersion,
		Present:     present,
		Deployments: deploys,
	}
}

func (s StaticProbe) ServerVersion(_ context.Context) (string, error) {
	if s.VersionErr != nil {
		return "", s.VersionErr
	}
	if strings.TrimSpace(s.Version) == "" {
		return "", fmt.Errorf("no kubernetes version")
	}
	return s.Version, nil
}

func (s StaticProbe) HasAPI(group, kind, _ string) bool {
	if s.Present == nil {
		return false
	}
	return s.Present[apiKey(group, kind)]
}

func (s StaticProbe) NeedsKamaji(_ context.Context) bool {
	return s.KamajiNeed
}

// Deployment returns a canned check. Unknown names are absent.
func (s StaticProbe) Deployment(_ context.Context, namespace, name string) ResourceCheck {
	if s.Deployments == nil {
		return ResourceCheck{Name: name, Message: "deployment not observed"}
	}
	if got, ok := s.Deployments[releaseKey(namespace, name)]; ok {
		return got
	}
	return ResourceCheck{Name: name, Message: "deployment not found"}
}

// RESTClusterProbe reads discovery and the REST mapper. It does not install
// anything.
type RESTClusterProbe struct {
	Mapper    RESTMappingProbe
	Discovery discovery.ServerVersionInterface
	Client    client.Client
}

func (p RESTClusterProbe) ServerVersion(_ context.Context) (string, error) {
	if p.Discovery == nil {
		return "", fmt.Errorf("no discovery client")
	}
	info, err := p.Discovery.ServerVersion()
	if err != nil {
		return "", err
	}
	if info == nil || strings.TrimSpace(info.GitVersion) == "" {
		return "", fmt.Errorf("empty kubernetes version")
	}
	return info.GitVersion, nil
}

func (p RESTClusterProbe) HasAPI(group, kind, version string) bool {
	if p.Mapper == nil {
		return false
	}
	_, err := p.Mapper.RESTMapping(schema.GroupKind{Group: group, Kind: kind}, version)
	return err == nil
}

// NeedsKamaji is true only when a DPUCluster of type kamaji is already on
// the cluster. Listing failure (no CRD, no RBAC) means Kamaji is not
// applicable: DPF marks it conditional, and this lab uses no DPUCluster.
func (p RESTClusterProbe) NeedsKamaji(ctx context.Context) bool {
	if p.Client == nil {
		return false
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(dpuClusterListGVK)
	if err := p.Client.List(ctx, list); err != nil {
		return false
	}
	for i := range list.Items {
		t, _, _ := unstructured.NestedString(list.Items[i].Object, string(IntentSpec), "type")
		if t == DepKamaji {
			return true
		}
	}
	return false
}

var deploymentGVK = schema.GroupVersionKind{
	Group:   "apps",
	Version: "v1",
	Kind:    "Deployment",
}

// Deployment reads one apps/v1 Deployment. Missing is Present=false.
func (p RESTClusterProbe) Deployment(ctx context.Context, namespace, name string) ResourceCheck {
	out := ResourceCheck{Name: namespace + "/" + name}
	if p.Client == nil {
		out.Message = "no client"
		return out
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(deploymentGVK)
	err := p.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		out.Message = "not found"
		return out
	}
	if err != nil {
		out.Message = err.Error()
		return out
	}
	out.Present = true
	replicas, found, _ := unstructured.NestedInt64(obj.Object, string(IntentSpec), "replicas")
	if !found {
		replicas = 1
	}
	ready, _, _ := unstructured.NestedInt64(obj.Object, statusKey, "readyReplicas")
	available, _, _ := unstructured.NestedInt64(obj.Object, statusKey, "availableReplicas")
	out.Healthy = ready >= replicas && (replicas == 0 || available > 0)
	if !out.Healthy {
		out.Message = fmt.Sprintf("ready %d/%d", ready, replicas)
	}
	return out
}

// DependencyReport is one preflight pass against cluster access, Kubernetes
// version, and the DPF-release prerequisites.
type DependencyReport struct {
	OK         bool
	Kubernetes string
	Missing    []string
	Message    string
}

// CheckDependencies validates cluster access, the pinned Kubernetes minor,
// and the DPF v26.4 prerequisites for NVIDIA (cert-manager, Argo CD, and
// Kamaji when a DPUCluster asks for it). A missing required item is Blocked.
// The checker never installs a chart.
func CheckDependencies(ctx context.Context, probe ClusterProbe, vendor string) DependencyReport {
	if err := ctx.Err(); err != nil {
		return DependencyReport{Message: err.Error()}
	}
	if probe == nil {
		return DependencyReport{
			Missing: []string{DepCluster},
			Message: clusterAccessMsg + ": no cluster probe",
		}
	}

	got, err := probe.ServerVersion(ctx)
	if err != nil {
		return DependencyReport{
			Missing: []string{DepCluster},
			Message: clusterAccessMsg,
		}
	}
	out := DependencyReport{Kubernetes: got}
	if !kubernetesMinorSupported(got) {
		out.Missing = []string{"kubernetes"}
		out.Message = fmt.Sprintf("Kubernetes %s is not the supported minor %s",
			got, kubernetesMinor(SupportedKubernetesVersion))
		return out
	}

	if vendor != VendorNVIDIA {
		out.OK = true
		out.Message = "cluster access and Kubernetes version are acceptable"
		return out
	}

	var missing []string
	if !probe.HasAPI(certManagerGroup, certManagerKind, certManagerVer) {
		missing = append(missing, DepCertManager)
	}
	if !probe.HasAPI(argoGroup, argoKind, argoVer) {
		missing = append(missing, DepArgoCD)
	}
	if probe.NeedsKamaji(ctx) && !probe.HasAPI(kamajiGroup, kamajiKind, kamajiVer) {
		missing = append(missing, DepKamaji)
	}
	if len(missing) > 0 {
		out.Missing = missing
		out.Message = fmt.Sprintf("DPF %s is blocked: missing %s",
			nvidiaDPFMin, strings.Join(missing, ", "))
		return out
	}
	out.OK = true
	out.Message = "DPF prerequisites are present"
	return out
}

func kubernetesMinorSupported(got string) bool {
	have, err := parseVersion(got)
	if err != nil {
		return false
	}
	want, err := parseVersion(SupportedKubernetesVersion)
	if err != nil {
		return false
	}
	return have.major == want.major && have.minor == want.minor
}

func kubernetesMinor(v string) string {
	parsed, err := parseVersion(v)
	if err != nil {
		return v
	}
	return fmt.Sprintf("v%d.%d", parsed.major, parsed.minor)
}

func apiKey(group, kind string) string {
	return group + "/" + kind
}
