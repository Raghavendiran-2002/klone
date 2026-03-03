package controller

import (
	"context"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
)

// ArgoCDDetector handles ArgoCD detection and credential management
type ArgoCDDetector struct {
	client    client.Client
	namespace string
}

// NewArgoCDDetector creates a new ArgoCD detector
func NewArgoCDDetector(c client.Client, namespace string) *ArgoCDDetector {
	return &ArgoCDDetector{
		client:    c,
		namespace: namespace,
	}
}

// IsArgoCDInstalled checks if ArgoCD is installed in the specified namespace
// It verifies the presence of both the namespace and the initial admin secret
func (d *ArgoCDDetector) IsArgoCDInstalled(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)

	// Check if ArgoCD namespace exists
	ns := &corev1.Namespace{}
	err := d.client.Get(ctx, types.NamespacedName{Name: d.namespace}, ns)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ArgoCD namespace not found", "namespace", d.namespace)
			return false, nil
		}
		return false, fmt.Errorf("failed to check for ArgoCD namespace: %w", err)
	}

	// Check if ArgoCD initial admin secret exists
	secret := &corev1.Secret{}
	err = d.client.Get(ctx, types.NamespacedName{
		Name:      "argocd-initial-admin-secret",
		Namespace: d.namespace,
	}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ArgoCD initial admin secret not found", "namespace", d.namespace)
			return false, nil
		}
		return false, fmt.Errorf("failed to check for ArgoCD secret: %w", err)
	}

	logger.Info("ArgoCD installation detected", "namespace", d.namespace)
	return true, nil
}

// GetArgoCDCredentials retrieves the ArgoCD admin password from the initial admin secret
func (d *ArgoCDDetector) GetArgoCDCredentials(ctx context.Context) (username, password string, err error) {
	secret := &corev1.Secret{}
	err = d.client.Get(ctx, types.NamespacedName{
		Name:      "argocd-initial-admin-secret",
		Namespace: d.namespace,
	}, secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to get ArgoCD credentials: %w", err)
	}

	// Get password from secret
	passwordBytes, ok := secret.Data["password"]
	if !ok {
		return "", "", fmt.Errorf("password not found in argocd-initial-admin-secret")
	}

	// ArgoCD admin username is always "admin"
	return "admin", string(passwordBytes), nil
}

// GetArgoCDServerURL retrieves the ArgoCD server URL from the argocd-server service
func (d *ArgoCDDetector) GetArgoCDServerURL(ctx context.Context) (string, error) {
	svc := &corev1.Service{}
	err := d.client.Get(ctx, types.NamespacedName{
		Name:      "argocd-server",
		Namespace: d.namespace,
	}, svc)
	if err != nil {
		return "", fmt.Errorf("failed to get ArgoCD server service: %w", err)
	}

	// Use internal service DNS name
	return fmt.Sprintf("argocd-server.%s.svc.cluster.local", d.namespace), nil
}

// GetClusterRegistrationName returns the name to use when registering the cluster in ArgoCD
func GetClusterRegistrationName(cluster *klonev1alpha1.KloneCluster) string {
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.ClusterName != "" {
		return cluster.Spec.ArgoCD.ClusterName
	}
	return fmt.Sprintf("klone-%s", cluster.Name)
}

// GetClusterLabels returns the labels to apply to the registered cluster in ArgoCD
func GetClusterLabels(cluster *klonev1alpha1.KloneCluster) map[string]string {
	labels := map[string]string{
		"cluster-type": "klone",
		"klone-name":   cluster.Name,
	}

	// Add custom labels if specified
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Labels != nil {
		for k, v := range cluster.Spec.ArgoCD.Labels {
			labels[k] = v
		}
	}

	return labels
}

// ShouldRegisterWithArgoCD determines if the cluster should be registered with ArgoCD
// based on spec configuration and auto-detection
func ShouldRegisterWithArgoCD(ctx context.Context, c client.Client, cluster *klonev1alpha1.KloneCluster) (bool, string, error) {
	// Determine ArgoCD namespace
	argoCDNamespace := "argocd"
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
		argoCDNamespace = cluster.Spec.ArgoCD.Namespace
	}

	// If explicitly disabled, return false
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Enabled != nil && !*cluster.Spec.ArgoCD.Enabled {
		return false, argoCDNamespace, nil
	}

	// If explicitly enabled, skip detection and return true
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Enabled != nil && *cluster.Spec.ArgoCD.Enabled {
		return true, argoCDNamespace, nil
	}

	// Auto-detect ArgoCD installation
	detector := NewArgoCDDetector(c, argoCDNamespace)
	installed, err := detector.IsArgoCDInstalled(ctx)
	if err != nil {
		return false, argoCDNamespace, fmt.Errorf("failed to detect ArgoCD: %w", err)
	}

	return installed, argoCDNamespace, nil
}

// ExtractKubeconfigFromControlPlane extracts the kubeconfig from the k3s control plane
// This returns a base64-encoded kubeconfig that can be used to connect to the nested cluster
func ExtractKubeconfigFromControlPlane(cluster *klonev1alpha1.KloneCluster) string {
	// The kubeconfig will be extracted by the Job's init container
	// This function returns the commands to extract it
	namespace := cluster.Name
	controlPlaneSvc := fmt.Sprintf("klone-controlplane.%s.svc.cluster.local", namespace)

	// Script to extract and modify kubeconfig
	script := fmt.Sprintf(`#!/bin/sh
set -e

# Wait for kubeconfig to be available
echo "Waiting for kubeconfig..."
while [ ! -f /var/lib/rancher/k3s/kubeconfig.yaml ]; do
  sleep 2
done

echo "Kubeconfig found, extracting..."

# Copy kubeconfig and modify server URL
cp /var/lib/rancher/k3s/kubeconfig.yaml /tmp/kubeconfig.yaml

# Replace localhost with service DNS name
sed -i 's|https://127.0.0.1:6443|https://%s:6443|g' /tmp/kubeconfig.yaml

# Add insecure-skip-tls-verify (k3s uses self-signed certs)
sed -i '/server:/a\    insecure-skip-tls-verify: true' /tmp/kubeconfig.yaml

# Remove certificate-authority-data to avoid cert validation issues
sed -i '/certificate-authority-data:/d' /tmp/kubeconfig.yaml

# Base64 encode the kubeconfig
cat /tmp/kubeconfig.yaml | base64 -w 0 > /shared/kubeconfig.b64

echo "Kubeconfig extracted and encoded"
`, controlPlaneSvc)

	return base64.StdEncoding.EncodeToString([]byte(script))
}
