package controller

import (
	"context"
	"time"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	RestartAnnotation        = "klone.io/restart-requested"
	RestartAnnotationApplied = "klone.io/restart-applied-at"
)

// handleRestartAnnotation checks if restart is requested and restarts workloads
func (r *KloneClusterReconciler) handleRestartAnnotation(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)

	// Check if restart is requested
	if cluster.Annotations == nil {
		return nil
	}

	restartRequested, exists := cluster.Annotations[RestartAnnotation]
	if !exists || restartRequested == "" {
		return nil
	}

	// Check if already applied
	restartApplied, appliedExists := cluster.Annotations[RestartAnnotationApplied]
	if appliedExists && restartApplied == restartRequested {
		// Already restarted for this request
		return nil
	}

	log.Info("Restart requested for cluster", "cluster", cluster.Name, "timestamp", restartRequested)

	namespaceName := GetNamespaceName(cluster.Name)
	restartTime := time.Now().Format(time.RFC3339)

	// Restart StatefulSet (control plane)
	ss := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetControlPlaneStatefulSetName(),
		Namespace: namespaceName,
	}, ss); err == nil {
		if ss.Spec.Template.Annotations == nil {
			ss.Spec.Template.Annotations = make(map[string]string)
		}
		ss.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartTime
		if err := r.Update(ctx, ss); err != nil {
			log.Error(err, "Failed to restart control plane StatefulSet")
		} else {
			log.Info("Restarted control plane StatefulSet")
		}
	}

	// Restart worker Deployment
	workerDep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetWorkerDeploymentName(),
		Namespace: namespaceName,
	}, workerDep); err == nil {
		if workerDep.Spec.Template.Annotations == nil {
			workerDep.Spec.Template.Annotations = make(map[string]string)
		}
		workerDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartTime
		if err := r.Update(ctx, workerDep); err != nil {
			log.Error(err, "Failed to restart worker Deployment")
		} else {
			log.Info("Restarted worker Deployment")
		}
	}

	// Restart terminal Deployment
	terminalDep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetTerminalDeploymentName(),
		Namespace: namespaceName,
	}, terminalDep); err == nil {
		if terminalDep.Spec.Template.Annotations == nil {
			terminalDep.Spec.Template.Annotations = make(map[string]string)
		}
		terminalDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartTime
		if err := r.Update(ctx, terminalDep); err != nil {
			log.Error(err, "Failed to restart terminal Deployment")
		} else {
			log.Info("Restarted terminal Deployment")
		}
	}

	// Mark restart as applied
	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}
	cluster.Annotations[RestartAnnotationApplied] = restartRequested
	if err := r.Update(ctx, cluster); err != nil {
		log.Error(err, "Failed to update restart annotation")
		return err
	}

	log.Info("Cluster restart completed", "cluster", cluster.Name)
	return nil
}
