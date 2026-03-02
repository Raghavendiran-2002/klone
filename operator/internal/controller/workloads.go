package controller

import (
	"fmt"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
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
		token = "supersecrettoken123"
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
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:    "clear-etcd",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c"},
							Args: []string{
								"rm -rf /var/lib/rancher/k3s/server/db/etcd",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "k3s-data",
									MountPath: "/var/lib/rancher/k3s",
								},
							},
						},
					},
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
		token = "supersecrettoken123"
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

# Configure kubectl context
export KUBECONFIG=/var/lib/rancher/k3s/kubeconfig.yaml

echo "Terminal ready!"
exec ttyd -p 80 -W bash
`

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetTerminalDeploymentName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":                "klone-terminal",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "klone-terminal",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "klone-terminal",
					},
				},
				Spec: corev1.PodSpec{
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
