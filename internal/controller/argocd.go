package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

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

// BuildArgoCDSecretReaderRole creates a Role in argocd namespace for reading repository secrets
func BuildArgoCDSecretReaderRole(cluster *klonev1alpha1.KloneCluster) *rbacv1.Role {
	argoCDNamespace := "argocd"
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
		argoCDNamespace = cluster.Spec.ArgoCD.Namespace
	}

	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("argocd-secret-reader-%s", cluster.Name),
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				"app":     "argocd-crd-installer",
				"cluster": cluster.Name,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list"},
			},
		},
	}
}

// BuildArgoCDSecretReaderRoleBinding creates a RoleBinding in argocd namespace
func BuildArgoCDSecretReaderRoleBinding(cluster *klonev1alpha1.KloneCluster) *rbacv1.RoleBinding {
	argoCDNamespace := "argocd"
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
		argoCDNamespace = cluster.Spec.ArgoCD.Namespace
	}

	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = cluster.Name
	}

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("argocd-secret-reader-%s", cluster.Name),
			Namespace: argoCDNamespace,
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
			Name:     fmt.Sprintf("argocd-secret-reader-%s", cluster.Name),
		},
	}
}

// buildImportHostArgoCDRepositorySecrets generates commands to import repository secrets from host cluster
func buildImportHostArgoCDRepositorySecrets(cluster *klonev1alpha1.KloneCluster) string {
	argoCDNamespace := "argocd"
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
		argoCDNamespace = cluster.Spec.ArgoCD.Namespace
	}

	// Generate script to copy secrets from host cluster to nested cluster
	return fmt.Sprintf(`echo "==== Importing ArgoCD repository secrets from host cluster ===="

# Debug: Check if we can access host cluster's argocd namespace
echo "Checking access to host cluster's %s namespace..."
if ! kubectl get namespace %s > /dev/null 2>&1; then
  echo "WARNING: Cannot access %s namespace in host cluster"
  echo "ArgoCD may not be installed in the host cluster"
else
  echo "Successfully accessed %s namespace in host cluster"
fi

# Get count of repository secrets in host cluster
echo "Searching for repository secrets in host cluster..."
SECRET_COUNT=$(kubectl get secrets -n %s -l argocd.argoproj.io/secret-type=repository --no-headers 2>/dev/null | wc -l | tr -d ' ')

if [ "$SECRET_COUNT" -gt 0 ]; then
  echo "Found $SECRET_COUNT repository secret(s) in host cluster"

  # List all secrets found
  echo "Secrets to import:"
  kubectl get secrets -n %s -l argocd.argoproj.io/secret-type=repository -o name 2>/dev/null

  # Get each secret and import to nested cluster
  kubectl get secrets -n %s -l argocd.argoproj.io/secret-type=repository -o name 2>/dev/null | while read -r secret_name; do
    SECRET_NAME=$(echo "$secret_name" | cut -d/ -f2)
    echo ""
    echo "Processing secret: $SECRET_NAME"

    # Get secret as YAML
    echo "Fetching secret from host cluster..."
    SECRET_YAML=$(kubectl get secret "$SECRET_NAME" -n %s -o yaml 2>&1)
    if [ $? -ne 0 ]; then
      echo "ERROR: Failed to fetch secret $SECRET_NAME from host cluster"
      echo "$SECRET_YAML"
      continue
    fi

    # Transform and apply to nested cluster
    echo "Applying secret to nested cluster..."
    echo "$SECRET_YAML" | \
      sed 's/namespace: .*/namespace: argocd/' | \
      sed '/resourceVersion:/d' | \
      sed '/uid:/d' | \
      sed '/creationTimestamp:/d' | \
      sed '/ownerReferences:/,+10d' | \
      kubectl exec -i -n %s $TERMINAL_POD -c terminal -- kubectl apply -f - 2>&1

    if [ $? -eq 0 ]; then
      echo "✓ Successfully imported: $SECRET_NAME"
    else
      echo "✗ Failed to import: $SECRET_NAME"
    fi
  done

  echo ""
  echo "Completed processing $SECRET_COUNT repository secret(s)"

  # Verify secrets in nested cluster
  echo ""
  echo "Verifying secrets in nested cluster..."
  kubectl exec -n %s $TERMINAL_POD -c terminal -- kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repository 2>&1 || echo "Warning: Could not verify secrets in nested cluster"
else
  echo "No repository secrets found in host cluster to import"
  echo "If you expected to find secrets, please check:"
  echo "  1. ArgoCD is installed in namespace: %s"
  echo "  2. Repository secrets exist with label: argocd.argoproj.io/secret-type=repository"
  echo "  3. The service account has permission to read secrets from that namespace"
fi

echo "==== Secret import process complete ===="
`, argoCDNamespace, argoCDNamespace, argoCDNamespace, argoCDNamespace, argoCDNamespace, argoCDNamespace, argoCDNamespace, argoCDNamespace, cluster.Status.Namespace, cluster.Status.Namespace, argoCDNamespace)
}

// buildArgoCDRepositorySecrets generates kubectl commands to create repository secrets
func buildArgoCDRepositorySecrets(cluster *klonev1alpha1.KloneCluster) string {
	var commands []string

	// First, import secrets from host cluster
	commands = append(commands, buildImportHostArgoCDRepositorySecrets(cluster))

	// Then, create any additional secrets defined in the spec
	if cluster.Spec.ArgoCD != nil && len(cluster.Spec.ArgoCD.Repositories) > 0 {
		commands = append(commands, "\necho \"Creating additional repository secrets from spec...\"")

		for i, repo := range cluster.Spec.ArgoCD.Repositories {
			secretName := repo.Name
			if secretName == "" {
				secretName = fmt.Sprintf("repo-%d", i)
			}

			// Build secret data based on authentication method
			var secretData string
			if repo.SSHPrivateKey != "" {
				// SSH-based authentication
				secretData = fmt.Sprintf(`  url: %s
  sshPrivateKey: |
%s
  type: %s`, repo.URL, indentString(repo.SSHPrivateKey, 4), repo.Type)
			} else {
				// HTTPS-based authentication
				secretData = fmt.Sprintf(`  url: %s`, repo.URL)
				if repo.Username != "" {
					secretData += fmt.Sprintf(`
  username: %s`, repo.Username)
				}
				if repo.Password != "" {
					secretData += fmt.Sprintf(`
  password: %s`, repo.Password)
				}
				secretData += fmt.Sprintf(`
  type: %s`, repo.Type)
			}

			secretYAML := fmt.Sprintf(`cat <<'REPOSECRET' | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repository
type: Opaque
stringData:
%s
REPOSECRET`, secretName, secretData)

			commands = append(commands, fmt.Sprintf(`echo "Creating repository secret: %s"
%s`, secretName, secretYAML))
		}
	}

	if len(commands) == 0 {
		return "echo \"No repository secrets to create\""
	}

	return strings.Join(commands, "\n")
}

// indentString indents each line of a string by the specified number of spaces
func indentString(s string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// NOTE: ArgoCD Helm installation has been removed.
// The operator now only handles cluster registration/deregistration with existing ArgoCD instances.
// Secret export functionality with labels (argocd.argoproj.io/secret-type=repository) is preserved
// via BuildArgoCDSecretReaderRole and BuildArgoCDSecretReaderRoleBinding functions.
