package controller

import (
	"context"
	"encoding/base64"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// BuildArgoCDCRDServiceAccount creates a ServiceAccount for the ArgoCD CRD installation job
func BuildArgoCDCRDServiceAccount(cluster *klonev1alpha1.KloneCluster) *corev1.ServiceAccount {
	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = cluster.Name
	}

	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-crd-installer",
			Namespace: namespace,
			Labels: map[string]string{
				"app":     "argocd-crd-installer",
				"cluster": cluster.Name,
			},
		},
	}
}

// BuildArgoCDCRDRole creates a Role for the ArgoCD CRD installation job
func BuildArgoCDCRDRole(cluster *klonev1alpha1.KloneCluster) *rbacv1.Role {
	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = cluster.Name
	}

	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-crd-installer",
			Namespace: namespace,
			Labels: map[string]string{
				"app":     "argocd-crd-installer",
				"cluster": cluster.Name,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "pods/exec"},
				Verbs:     []string{"get", "list", "create"},
			},
		},
	}
}

// BuildArgoCDCRDRoleBinding creates a RoleBinding for the ArgoCD CRD installation job
func BuildArgoCDCRDRoleBinding(cluster *klonev1alpha1.KloneCluster) *rbacv1.RoleBinding {
	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = cluster.Name
	}

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-crd-installer",
			Namespace: namespace,
			Labels: map[string]string{
				"app":     "argocd-crd-installer",
				"cluster": cluster.Name,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "argocd-crd-installer",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "argocd-crd-installer",
		},
	}
}

// BuildArgoCDCRDInstallJob creates a Job that installs ArgoCD CRDs into the nested K3s cluster
func BuildArgoCDCRDInstallJob(cluster *klonev1alpha1.KloneCluster) *batchv1.Job {
	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = cluster.Name
	}

	ttl := int32(300)            // Clean up after 5 minutes
	backoffLimit := int32(2)     // Retry twice
	activeDeadline := int64(600) // Timeout after 10 minutes

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "install-argocd-crds",
			Namespace: namespace,
			Labels: map[string]string{
				"app":         "argocd-crd-installer",
				"cluster":     cluster.Name,
				"managed-by":  "klone-operator",
			},
			// Note: Cannot set owner reference across namespaces
			// Job will be cleaned up via TTLSecondsAfterFinished
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "argocd-crd-installer",
						"cluster": cluster.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "argocd-crd-installer",
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "install-argocd-crds",
							Image:   "bitnami/kubectl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{fmt.Sprintf(`
set -e

echo "Waiting for terminal pod to be ready..."
TERMINAL_POD=""
for i in $(seq 1 60); do
  TERMINAL_POD=$(kubectl get pod -n %s -l app=klone-terminal -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
  if [ -n "$TERMINAL_POD" ]; then
    POD_STATUS=$(kubectl get pod -n %s $TERMINAL_POD -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [ "$POD_STATUS" = "Running" ]; then
      echo "Terminal pod found and running: $TERMINAL_POD"
      break
    fi
  fi
  echo "Terminal pod not ready yet (attempt $i/60), waiting..."
  sleep 5
done

if [ -z "$TERMINAL_POD" ]; then
  echo "ERROR: Terminal pod not found after 5 minutes"
  exit 1
fi

echo "Installing ArgoCD CRDs in nested K3s cluster..."

# Install ArgoCD CRDs from the official manifest
kubectl exec -n %s $TERMINAL_POD -c terminal -- sh -c '
set -e
echo "Downloading ArgoCD CRDs..."
kubectl apply --server-side -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/crds/application-crd.yaml
kubectl apply --server-side -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/crds/applicationset-crd.yaml
kubectl apply --server-side -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/crds/appproject-crd.yaml

echo "Verifying ArgoCD CRDs installation..."
kubectl get crd applications.argoproj.io
kubectl get crd applicationsets.argoproj.io
kubectl get crd appprojects.argoproj.io

echo "ArgoCD CRDs installed successfully!"
'

echo "ArgoCD CRDs installation complete for cluster %s"
`, namespace, namespace, namespace, cluster.Name)},
						},
					},
				},
			},
		},
	}

	return job
}
