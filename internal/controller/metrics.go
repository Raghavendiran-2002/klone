package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
// Note: K3s has metrics-server built-in, so we don't need to install it in nested clusters
// We only need to ensure the host cluster has metrics-server installed
)

// EnsureHostMetricsServer checks if metrics-server is installed in the host cluster
// and installs it if not present
func (r *KloneClusterReconciler) EnsureHostMetricsServer(ctx context.Context) error {
	log := logf.FromContext(ctx)

	// Check if metrics-server deployment exists in kube-system namespace
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      "metrics-server",
		Namespace: "kube-system",
	}, deployment)

	if err == nil {
		log.V(1).Info("Metrics-server already installed in host cluster")
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for metrics-server: %w", err)
	}

	// Metrics-server not found, check if we should install it
	log.Info("Metrics-server not found in host cluster, checking for installation job")

	// Check if installation job already exists
	jobName := "install-host-metrics-server"
	existingJob := &batchv1.Job{}
	jobKey := types.NamespacedName{
		Namespace: "klone",
		Name:      jobName,
	}

	err = r.Get(ctx, jobKey, existingJob)
	if err == nil {
		// Job exists, check status
		if existingJob.Status.Succeeded > 0 {
			log.Info("Host metrics-server installation job completed successfully")
			return nil
		} else if existingJob.Status.Failed > 0 {
			log.Info("Host metrics-server installation job failed, will not retry automatically")
			return nil
		}
		log.V(1).Info("Host metrics-server installation job is running")
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Create installation job for host cluster
	log.Info("Creating host metrics-server installation job")

	job := BuildHostMetricsServerInstallJob()

	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create host metrics-server installation job: %w", err)
	}

	log.Info("Created host metrics-server installation job")
	return nil
}

// BuildHostMetricsServerInstallJob creates a Job that installs metrics-server into the host cluster
func BuildHostMetricsServerInstallJob() *batchv1.Job {
	ttl := int32(300)            // Clean up after 5 minutes
	backoffLimit := int32(2)     // Retry twice
	activeDeadline := int64(600) // Timeout after 10 minutes

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "install-host-metrics-server",
			Namespace: "klone",
			Labels: map[string]string{
				"app": "host-metrics-server-installer",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "host-metrics-server-installer",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "klone-controller-manager",
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "install-metrics-server",
							Image:   "bitnami/kubectl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{`
set -e

echo "Checking if metrics-server is already installed in host cluster..."
if kubectl get deployment metrics-server -n kube-system > /dev/null 2>&1; then
  echo "Metrics-server is already installed, skipping installation"
  exit 0
fi

echo "Installing metrics-server in host cluster..."

# Download and apply official metrics-server manifest
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.7.2/components.yaml

# Patch metrics-server to work with k3s (add --kubelet-insecure-tls flag)
echo "Patching metrics-server for k3s compatibility..."
kubectl patch deployment metrics-server -n kube-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--kubelet-insecure-tls"
  }
]'

echo "Waiting for metrics-server to be ready..."
kubectl wait --for=condition=available --timeout=180s deployment/metrics-server -n kube-system || true

echo "Host metrics-server installation complete!"

# Test the metrics API
echo "Testing metrics API..."
sleep 15
kubectl top nodes || echo "Warning: metrics API may take a few more seconds to be fully ready"
`},
						},
					},
				},
			},
		},
	}

	return job
}
