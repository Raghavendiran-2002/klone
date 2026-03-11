/*
Copyright 2026.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KloneClusterSpec defines the desired state of KloneCluster
type KloneClusterSpec struct {
	// k3s configuration for the nested cluster
	// +required
	K3s K3sSpec `json:"k3s"`

	// storage configuration for persistent volumes
	// +required
	Storage StorageSpec `json:"storage"`

	// terminal configuration for web-based access
	// +optional
	Terminal *TerminalSpec `json:"terminal,omitempty"`

	// ingress configuration for external access
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// networking configuration for cluster and service CIDRs
	// +optional
	Networking *NetworkingSpec `json:"networking,omitempty"`

	// metricsServer configuration for auto-installation
	// +optional
	MetricsServer *MetricsServerSpec `json:"metricsServer,omitempty"`

	// argoCD configuration for cluster registration with host ArgoCD
	// +optional
	ArgoCD *ArgoCDSpec `json:"argoCD,omitempty"`

	// username for cluster access authentication
	// If not specified, defaults to the cluster name
	// +optional
	Username string `json:"username,omitempty"`
}

// K3sSpec defines k3s cluster configuration
type K3sSpec struct {
	// image is the k3s container image
	// +kubebuilder:default="rancher/k3s:v1.35.1-k3s1"
	// +optional
	Image string `json:"image,omitempty"`

	// token is the shared secret for k3s agent authentication
	// +kubebuilder:default="supersecrettoken123"
	// +optional
	Token string `json:"token,omitempty"`

	// controlPlane configuration
	// +optional
	ControlPlane *ControlPlaneSpec `json:"controlPlane,omitempty"`

	// worker configuration
	// +optional
	Worker *WorkerSpec `json:"worker,omitempty"`
}

// ControlPlaneSpec defines control plane configuration
type ControlPlaneSpec struct {
	// replicas is the number of control plane instances
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// resources for control plane pods
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// WorkerSpec defines worker node configuration
type WorkerSpec struct {
	// replicas is the number of worker nodes
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// resources for worker pods
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// ResourceRequirements defines resource requests and limits
type ResourceRequirements struct {
	// requests describes the minimum resources required
	// +optional
	Requests map[string]string `json:"requests,omitempty"`

	// limits describes the maximum resources allowed
	// +optional
	Limits map[string]string `json:"limits,omitempty"`
}

// StorageSpec defines storage configuration
type StorageSpec struct {
	// storageClass for PVC
	// +kubebuilder:default="local-path"
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// size of the persistent volume
	// +kubebuilder:default="5Gi"
	// +optional
	Size string `json:"size,omitempty"`

	// hostPath base directory for cluster data
	// +kubebuilder:default="/home/raghav/klone"
	// +optional
	HostPath string `json:"hostPath,omitempty"`

	// nodeAffinity for PV binding
	// +optional
	NodeAffinity *NodeAffinitySpec `json:"nodeAffinity,omitempty"`
}

// NodeAffinitySpec defines node affinity for storage
type NodeAffinitySpec struct {
	// enabled determines if node affinity is used
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// label is the node label key for affinity (e.g., workload=primary)
	// +kubebuilder:default="primary"
	// +optional
	Label string `json:"label,omitempty"`
}

// TerminalSpec defines web terminal configuration
type TerminalSpec struct {
	// image for the terminal container
	// +kubebuilder:default="alpine:3.19"
	// +optional
	Image string `json:"image,omitempty"`

	// replicas is the number of terminal instances
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// resources for terminal pod
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// IngressSpec defines ingress configuration
type IngressSpec struct {
	// type of ingress: tailscale, loadbalancer, or none
	// +kubebuilder:validation:Enum=tailscale;loadbalancer;none
	// +kubebuilder:default="tailscale"
	// +optional
	Type string `json:"type,omitempty"`

	// tailscale specific configuration
	// +optional
	Tailscale *TailscaleIngressSpec `json:"tailscale,omitempty"`

	// loadBalancer specific configuration
	// +optional
	LoadBalancer *LoadBalancerIngressSpec `json:"loadBalancer,omitempty"`
}

// TailscaleIngressSpec defines Tailscale ingress configuration
type TailscaleIngressSpec struct {
	// domain is the Tailscale network domain
	// +optional
	Domain string `json:"domain,omitempty"`

	// tags for Tailscale device
	// +optional
	Tags []string `json:"tags,omitempty"`

	// annotations for the Ingress resource
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// LoadBalancerIngressSpec defines AWS ALB ingress configuration
type LoadBalancerIngressSpec struct {
	// scheme is the load balancer scheme (internet-facing or internal)
	// +kubebuilder:validation:Enum=internet-facing;internal
	// +kubebuilder:default="internet-facing"
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// certificateArn for HTTPS listener
	// +optional
	CertificateArn string `json:"certificateArn,omitempty"`

	// externalDNS configuration
	// +optional
	ExternalDNS *ExternalDNSSpec `json:"externalDNS,omitempty"`

	// annotations for the Ingress resource
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ExternalDNSSpec defines external-dns configuration
type ExternalDNSSpec struct {
	// hostname for DNS record
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// ttl for DNS record
	// +kubebuilder:default=300
	// +optional
	TTL int32 `json:"ttl,omitempty"`
}

// NetworkingSpec defines network configuration
type NetworkingSpec struct {
	// clusterCIDR for pod network (auto-generated if empty)
	// +optional
	ClusterCIDR string `json:"clusterCIDR,omitempty"`

	// serviceCIDR for service network (auto-generated if empty)
	// +optional
	ServiceCIDR string `json:"serviceCIDR,omitempty"`
}

// MetricsServerSpec defines metrics-server auto-install configuration
type MetricsServerSpec struct {
	// enabled determines if metrics-server should be auto-installed
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// image for metrics-server
	// +kubebuilder:default="registry.k8s.io/metrics-server/metrics-server:v0.7.0"
	// +optional
	Image string `json:"image,omitempty"`
}

// ArgoCDSpec defines ArgoCD integration configuration
type ArgoCDSpec struct {
	// enabled determines if the cluster should be registered with host ArgoCD
	// If not specified, auto-detection is used (checks for ArgoCD in host cluster)
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// namespace is the namespace where ArgoCD is installed in the host cluster
	// +kubebuilder:default="argocd"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// labels are additional labels to add to the registered cluster in ArgoCD
	// Default label 'cluster-type=klone' is always added
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// clusterName is the name to use when registering the cluster in ArgoCD
	// If not specified, defaults to "klone-{cluster-name}"
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// repositories defines Git repository secrets to create in the nested cluster
	// These secrets will be created in the argocd namespace with proper labels
	// +optional
	Repositories []ArgoCDRepository `json:"repositories,omitempty"`
}

// ArgoCDRepository defines a Git repository secret for ArgoCD
type ArgoCDRepository struct {
	// name of the repository secret
	// +optional
	Name string `json:"name,omitempty"`

	// url of the Git repository
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// username for repository authentication
	// +optional
	Username string `json:"username,omitempty"`

	// password or token for repository authentication
	// +optional
	Password string `json:"password,omitempty"`

	// sshPrivateKey for SSH-based authentication
	// +optional
	SSHPrivateKey string `json:"sshPrivateKey,omitempty"`

	// type of repository (git, helm)
	// +kubebuilder:default="git"
	// +optional
	Type string `json:"type,omitempty"`
}

// KloneClusterStatus defines the observed state of KloneCluster.
type KloneClusterStatus struct {
	// phase represents the current lifecycle phase of the cluster
	// +kubebuilder:validation:Enum=Creating;Running;Terminating;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// conditions represent the current state of the KloneCluster resource.
	// Condition types:
	// - "Ready": all components are running and healthy
	// - "TerminalReady": terminal pod is ready and accessible
	// - "IngressReady": ingress is configured and accessible
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// workloads tracks the status of created workloads
	// +optional
	Workloads []WorkloadStatus `json:"workloads,omitempty"`

	// ingressURL is the URL to access the cluster terminal
	// +optional
	IngressURL string `json:"ingressURL,omitempty"`

	// loadBalancerHostname is the hostname of the load balancer (if using ALB)
	// +optional
	LoadBalancerHostname string `json:"loadBalancerHostname,omitempty"`

	// namespace is the namespace where cluster resources are deployed
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// persistentVolume is the name of the PV created for this cluster
	// +optional
	PersistentVolume string `json:"persistentVolume,omitempty"`

	// metricsServerInstalled indicates if metrics-server was successfully installed
	// +optional
	MetricsServerInstalled bool `json:"metricsServerInstalled,omitempty"`

	// clusterCIDR is the actual CIDR assigned to the cluster
	// +optional
	ClusterCIDR string `json:"clusterCIDR,omitempty"`

	// serviceCIDR is the actual CIDR assigned for services
	// +optional
	ServiceCIDR string `json:"serviceCIDR,omitempty"`

	// argoCDRegistered indicates if the cluster has been registered with host ArgoCD
	// +optional
	ArgoCDRegistered bool `json:"argoCDRegistered,omitempty"`

	// argoCDCRDsInstalled indicates if ArgoCD CRDs have been installed in the nested cluster
	// +optional
	ArgoCDCRDsInstalled bool `json:"argoCDCRDsInstalled,omitempty"`

	// argoCDClusterName is the name used when registering the cluster in ArgoCD
	// +optional
	ArgoCDClusterName string `json:"argoCDClusterName,omitempty"`

	// credentialsSecretName is the name of the secret containing cluster credentials (username/password)
	// +optional
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`

	// currentNode is the node where the cluster workloads are currently scheduled
	// +optional
	CurrentNode string `json:"currentNode,omitempty"`

	// lastRelocationTime is the timestamp of the last cluster relocation
	// +optional
	LastRelocationTime string `json:"lastRelocationTime,omitempty"`

	// relocationReason is the reason for the last relocation
	// +optional
	RelocationReason string `json:"relocationReason,omitempty"`
}

// WorkloadStatus represents the status of a workload
type WorkloadStatus struct {
	// kind is the workload kind (StatefulSet or Deployment)
	// +optional
	Kind string `json:"kind,omitempty"`

	// name is the workload name
	// +optional
	Name string `json:"name,omitempty"`

	// ready is the number of ready replicas
	// +optional
	Ready int32 `json:"ready,omitempty"`

	// desired is the desired number of replicas
	// +optional
	Desired int32 `json:"desired,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KloneCluster is the Schema for the kloneclusters API
type KloneCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KloneCluster
	// +required
	Spec KloneClusterSpec `json:"spec"`

	// status defines the observed state of KloneCluster
	// +optional
	Status KloneClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KloneClusterList contains a list of KloneCluster
type KloneClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KloneCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KloneCluster{}, &KloneClusterList{})
}
