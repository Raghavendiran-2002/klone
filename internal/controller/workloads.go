package controller

import (
	"fmt"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildControlPlaneStatefulSet creates the k3s control plane StatefulSet
func BuildControlPlaneStatefulSet(cluster *klonev1alpha1.KloneCluster) *appsv1.StatefulSet {
	namespaceName := GetNamespaceName(cluster.Name)

	// Get configuration with defaults
	image := cluster.Spec.K3s.Image
	if image == "" {
		image = "rancher/k3s:v1.35.1-k3s1"
	}

	token := cluster.Spec.K3s.Token
	if token == "" {
		token = DefaultK3sToken
	}

	replicas := int32(1)
	if cluster.Spec.K3s.ControlPlane != nil && cluster.Spec.K3s.ControlPlane.Replicas > 0 {
		replicas = cluster.Spec.K3s.ControlPlane.Replicas
	}

	// Allocate CIDRs
	clusterCIDR := cluster.Status.ClusterCIDR
	serviceCIDR := cluster.Status.ServiceCIDR
	if clusterCIDR == "" || serviceCIDR == "" {
		clusterCIDR, serviceCIDR = AllocateCIDRs(cluster.Name)
	}

	// Build k3s server args
	k3sArgs := []string{
		"server",
		fmt.Sprintf("--cluster-cidr=%s", clusterCIDR),
		fmt.Sprintf("--service-cidr=%s", serviceCIDR),
		"--flannel-backend=vxlan",
		"--disable=traefik",
		"--disable=servicelb",
		"--disable=local-storage",
		"--kube-apiserver-arg=feature-gates=",
		fmt.Sprintf("--token=%s", token),
		"--kube-controller-manager-arg=terminated-pod-gc-threshold=10",
		"--kubelet-arg=feature-gates=",
		"--kubelet-arg=allowed-unsafe-sysctls=net.*",
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetControlPlaneStatefulSetName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":                "k3s-control-plane",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: GetControlPlaneServiceName(),
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "k3s-control-plane",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "k3s-control-plane",
					},
					Annotations: map[string]string{
						"container.apparmor.security.beta.kubernetes.io/k3s": "unconfined",
						"container.seccomp.security.alpha.kubernetes.io/k3s": "unconfined",
					},
				},
				Spec: corev1.PodSpec{
					HostPID:     true,
					HostIPC:     true,
					HostNetwork: false,
					Containers: []corev1.Container{
						{
							Name:  "k3s",
							Image: image,
							Args:  k3sArgs,
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
							},
							Env: []corev1.EnvVar{
								{
									Name:  "K3S_KUBECONFIG_OUTPUT",
									Value: "/var/lib/rancher/k3s/kubeconfig.yaml",
								},
								{
									Name:  "K3S_KUBECONFIG_MODE",
									Value: "644",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "k3s-data",
									MountPath: "/var/lib/rancher/k3s",
								},
								{
									Name:      "dev",
									MountPath: "/dev",
								},
								{
									Name:      "sys",
									MountPath: "/sys",
								},
								{
									Name:      "lib-modules",
									MountPath: "/lib/modules",
									ReadOnly:  true,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "api",
									ContainerPort: 6443,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "k3s-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: GetPVCName(),
								},
							},
						},
						{
							Name: "dev",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
								},
							},
						},
						{
							Name: "sys",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys",
								},
							},
						},
						{
							Name: "lib-modules",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/lib/modules",
								},
							},
						},
					},
				},
			},
		},
	}

	// Add resource requirements if specified
	if cluster.Spec.K3s.ControlPlane != nil && cluster.Spec.K3s.ControlPlane.Resources != nil {
		resources := buildResourceRequirements(cluster.Spec.K3s.ControlPlane.Resources)
		statefulSet.Spec.Template.Spec.Containers[0].Resources = resources
	}

	return statefulSet
}

// BuildWorkerDeployment creates the k3s worker Deployment
func BuildWorkerDeployment(cluster *klonev1alpha1.KloneCluster) *appsv1.Deployment {
	namespaceName := GetNamespaceName(cluster.Name)

	// Get configuration with defaults
	image := cluster.Spec.K3s.Image
	if image == "" {
		image = "rancher/k3s:v1.35.1-k3s1"
	}

	token := cluster.Spec.K3s.Token
	if token == "" {
		token = DefaultK3sToken
	}

	replicas := int32(2)
	if cluster.Spec.K3s.Worker != nil && cluster.Spec.K3s.Worker.Replicas >= 0 {
		replicas = cluster.Spec.K3s.Worker.Replicas
	}

	// Control plane URL
	controlPlaneURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:6443",
		GetControlPlaneServiceName(), namespaceName)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetWorkerDeploymentName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":                "k3s-worker",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "k3s-worker",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "k3s-worker",
					},
				},
				Spec: corev1.PodSpec{
					// Ensure worker is scheduled on same node as control plane
					Affinity: &corev1.Affinity{
						PodAffinity: &corev1.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": "k3s-control-plane",
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "k3s",
							Image: image,
							Args: []string{
								"agent",
								fmt.Sprintf("--server=%s", controlPlaneURL),
								fmt.Sprintf("--token=%s", token),
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "lib-modules",
									MountPath: "/lib/modules",
									ReadOnly:  true,
								},
								{
									Name:      "cgroup",
									MountPath: "/sys/fs/cgroup",
								},
								{
									Name:      "run",
									MountPath: "/run",
								},
								{
									Name:      "varrun",
									MountPath: "/var/run",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "lib-modules",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/lib/modules",
								},
							},
						},
						{
							Name: "cgroup",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys/fs/cgroup",
								},
							},
						},
						{
							Name: "run",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "varrun",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Add resource requirements if specified
	if cluster.Spec.K3s.Worker != nil && cluster.Spec.K3s.Worker.Resources != nil {
		resources := buildResourceRequirements(cluster.Spec.K3s.Worker.Resources)
		deployment.Spec.Template.Spec.Containers[0].Resources = resources
	}

	return deployment
}

// BuildTerminalDeployment creates the terminal Deployment with kubectl and ttyd
func BuildTerminalDeployment(cluster *klonev1alpha1.KloneCluster) *appsv1.Deployment {
	namespaceName := GetNamespaceName(cluster.Name)

	// Get configuration with defaults
	image := "alpine:3.19"
	if cluster.Spec.Terminal != nil && cluster.Spec.Terminal.Image != "" {
		image = cluster.Spec.Terminal.Image
	}

	replicas := int32(1)
	if cluster.Spec.Terminal != nil && cluster.Spec.Terminal.Replicas > 0 {
		replicas = cluster.Spec.Terminal.Replicas
	}

	token := cluster.Spec.K3s.Token
	if token == "" {
		token = DefaultK3sToken
	}

	// Terminal setup script with caching
	setupScript := `#!/bin/sh
set -e

CACHE=/k3s/term-cache

# Install dependencies
apk add --no-cache curl bash

# Detect architecture
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
  KUBECTL_ARCH="arm64"
  TTYD_ARCH="aarch64"
else
  KUBECTL_ARCH="amd64"
  TTYD_ARCH="x86_64"
fi

# Cache kubectl
if [ ! -x "$CACHE/kubectl" ]; then
  echo "Caching kubectl (first run)..."
  curl -Lo $CACHE/kubectl https://dl.k8s.io/release/v1.35.0/bin/linux/${KUBECTL_ARCH}/kubectl
  chmod +x $CACHE/kubectl
fi
ln -sf $CACHE/kubectl /usr/local/bin/kubectl

# Cache ttyd
if [ ! -x "$CACHE/ttyd" ]; then
  echo "Caching ttyd (first run)..."
  curl -Lo $CACHE/ttyd https://github.com/tsl0922/ttyd/releases/download/1.7.7/ttyd.${TTYD_ARCH}
  chmod +x $CACHE/ttyd
fi
ln -sf $CACHE/ttyd /usr/local/bin/ttyd

# Configure kubectl to use k3s cluster - copy to default location
mkdir -p ~/.kube
cp /shared/kubeconfig.yaml ~/.kube/config
chmod 600 ~/.kube/config
export KUBECONFIG=~/.kube/config
echo "Kubeconfig copied to ~/.kube/config"

# Wait for k3s API to be ready (simple connectivity check)
echo "Waiting for k3s API server..."
max_attempts=30
attempt=0
until curl -k -s https://k3s-control-plane.` + namespaceName + `.svc.cluster.local:6443/healthz > /dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ $attempt -ge $max_attempts ]; then
    echo "Warning: k3s API server not responding, but starting terminal anyway"
    break
  fi
  sleep 2
done
echo "k3s API server is ready!"

# Set KUBECONFIG and alias in .bashrc so they persist in terminal sessions
echo "export KUBECONFIG=~/.kube/config" >> ~/.bashrc
echo "alias k=kubectl" >> ~/.bashrc

echo "Terminal ready! Access via ttyd on port 80"
echo "Try 'kubectl get nodes' or 'k get nodes' to interact with the nested k3s cluster"
exec ttyd -p 80 -W bash
`

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetTerminalDeploymentName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":                TerminalDeploymentName,
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": TerminalDeploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": TerminalDeploymentName,
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: boolPtr(false),
					// Ensure terminal is scheduled on same node as control plane (for hostPath PVC access)
					Affinity: &corev1.Affinity{
						PodAffinity: &corev1.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": "k3s-control-plane",
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:    "setup-kubeconfig",
							Image:   image,
							Command: []string{"/bin/sh", "-c"},
							Args: []string{fmt.Sprintf(`
set -e
apk add --no-cache sed
# Wait for kubeconfig to be available and freshly generated
echo "Waiting for kubeconfig..."
while [ ! -f /k3s-config/kubeconfig.yaml ]; do
  sleep 2
done

# Wait for k3s to finish writing kubeconfig with fresh certificates
echo "Waiting for k3s to finish writing kubeconfig..."
sleep 15

# Copy kubeconfig to shared volume and modify for service access
cp /k3s-config/kubeconfig.yaml /shared/kubeconfig.yaml
# Replace server URL from 127.0.0.1 to fully qualified service DNS name
sed -i 's|https://127.0.0.1:6443|https://k3s-control-plane.%s.svc.cluster.local:6443|g' /shared/kubeconfig.yaml
# Remove certificate-authority-data line
sed -i '/certificate-authority-data:/d' /shared/kubeconfig.yaml
# Add insecure-skip-tls-verify after server line
sed -i '/server: https:\/\/k3s-control-plane.%s.svc.cluster.local:6443/a\    insecure-skip-tls-verify: true' /shared/kubeconfig.yaml
echo "Kubeconfig ready!"
`, namespaceName, namespaceName)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "k3s-data",
									MountPath: "/k3s-config",
									ReadOnly:  true,
								},
								{
									Name:      "shared-config",
									MountPath: "/shared",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "terminal",
							Image:   image,
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{setupScript},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "K3S_TOKEN",
									Value: token,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "k3s-data",
									MountPath: "/var/lib/rancher/k3s",
									ReadOnly:  true,
								},
								{
									Name:      "k3s-data",
									MountPath: "/k3s/term-cache",
									SubPath:   "term-cache",
								},
								{
									Name:      "shared-config",
									MountPath: "/shared",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "k3s-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: GetPVCName(),
								},
							},
						},
						{
							Name: "shared-config",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Add resource requirements if specified
	if cluster.Spec.Terminal != nil && cluster.Spec.Terminal.Resources != nil {
		resources := buildResourceRequirements(cluster.Spec.Terminal.Resources)
		deployment.Spec.Template.Spec.Containers[0].Resources = resources
	}

	return deployment
}

// buildResourceRequirements converts CRD resource spec to k8s ResourceRequirements
func buildResourceRequirements(res *klonev1alpha1.ResourceRequirements) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}

	if res.Requests != nil {
		for k, v := range res.Requests {
			requirements.Requests[corev1.ResourceName(k)] = resource.MustParse(v)
		}
	}

	if res.Limits != nil {
		for k, v := range res.Limits {
			requirements.Limits[corev1.ResourceName(k)] = resource.MustParse(v)
		}
	}

	return requirements
}

// BuildArgoCDRegisterJob creates a Job that registers the nested k3s cluster with host ArgoCD
func BuildArgoCDRegisterJob(cluster *klonev1alpha1.KloneCluster, argoCDNamespace, username, password string) *batchv1.Job {
	namespaceName := GetNamespaceName(cluster.Name)
	clusterName := GetClusterRegistrationName(cluster)
	labels := GetClusterLabels(cluster)

	// Build label arguments for argocd CLI
	labelArgs := ""
	for k, v := range labels {
		labelArgs += fmt.Sprintf(" --label %s=%s", k, v)
	}

	// Job name
	jobName := fmt.Sprintf("argocd-register-%s", cluster.Name)

	// TTL for job cleanup (5 minutes after completion)
	ttl := int32(300)

	// Backoff limit (retry 3 times)
	backoffLimit := int32(3)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":           "argocd-register",
				"klone-cluster": cluster.Name,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":           "argocd-register",
						"klone-cluster": cluster.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "klone-argocd-registration",
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					InitContainers: []corev1.Container{
						{
							Name:    "extract-kubeconfig",
							Image:   "bitnami/kubectl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
set -e
echo "Extracting kubeconfig from control plane pod..."

# Wait for control plane pod to be ready
for i in $(seq 1 30); do
  if kubectl get pod -n %s k3s-control-plane-0 -o jsonpath='{.status.phase}' 2>/dev/null | grep -q Running; then
    echo "Control plane pod is running"
    break
  fi
  echo "Waiting for control plane pod... ($i/30)"
  sleep 5
done

# Extract kubeconfig using kubectl cp
kubectl cp %s/k3s-control-plane-0:/var/lib/rancher/k3s/kubeconfig.yaml /shared/kubeconfig.yaml

# Replace localhost with service DNS name (sed is pre-installed in bitnami/kubectl)
sed -i 's|https://127.0.0.1:6443|https://k3s-control-plane.%s.svc.cluster.local:6443|g' /shared/kubeconfig.yaml

# Add insecure-skip-tls-verify (k3s uses self-signed certs)
sed -i '/server:/a\    insecure-skip-tls-verify: true' /shared/kubeconfig.yaml

# Remove certificate-authority-data
sed -i '/certificate-authority-data:/d' /shared/kubeconfig.yaml

echo "Kubeconfig extracted and configured successfully"
`, namespaceName, namespaceName, namespaceName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "shared-config",
									MountPath: "/shared",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "argocd-register",
							Image:   "quay.io/argoproj/argocd:v2.9.3",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
set -e

# Setup kubeconfig for nested cluster
export KUBECONFIG=/shared/kubeconfig.yaml

# Wait a bit for the nested cluster to stabilize
echo "Waiting for nested cluster to stabilize..."
sleep 15

# Login to ArgoCD
echo "Logging in to ArgoCD..."
echo "y" | argocd login argocd-server.%s.svc.cluster.local:443 \
  --username=%s \
  --password=%s \
  --insecure \
  --grpc-web

# Register cluster with ArgoCD (argocd cluster add will validate connectivity)
echo "Registering cluster with ArgoCD..."
echo "Cluster name: %s"
echo "Labels: %s"

# The argocd cluster add command will create/update the cluster
# Using --upsert ensures idempotency
argocd cluster add default \
  --name=%s \
  --kubeconfig=/shared/kubeconfig.yaml \
  --upsert \
  --yes%s

echo "Verifying cluster registration..."
argocd cluster get %s || echo "Warning: Could not verify cluster registration"

echo "ArgoCD registration complete!"
`, argoCDNamespace, username, password, clusterName, labelArgs, clusterName, labelArgs, clusterName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "shared-config",
									MountPath: "/shared",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "shared-config",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	return job
}
