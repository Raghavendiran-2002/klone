package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
)

// relocateClusterToNode orchestrates the full relocation of cluster workloads to a target node
func (r *KloneClusterReconciler) relocateClusterToNode(ctx context.Context, cluster *klonev1alpha1.KloneCluster, targetNode string) error {
	log := logf.FromContext(ctx)
	namespaceName := cluster.Name

	log.Info("Starting cluster relocation", "cluster", cluster.Name, "targetNode", targetNode)

	// Step 1: Delete terminal deployment
	if err := r.deleteDeployment(ctx, namespaceName, GetTerminalDeploymentName()); err != nil {
		return fmt.Errorf("failed to delete terminal deployment: %w", err)
	}
	log.Info("Deleted terminal deployment")

	// Step 2: Delete worker deployment
	if err := r.deleteDeployment(ctx, namespaceName, GetWorkerDeploymentName()); err != nil {
		return fmt.Errorf("failed to delete worker deployment: %w", err)
	}
	log.Info("Deleted worker deployment")

	// Step 3: Delete control plane statefulset
	if err := r.deleteStatefulSet(ctx, namespaceName, GetControlPlaneStatefulSetName()); err != nil {
		return fmt.Errorf("failed to delete control plane statefulset: %w", err)
	}
	log.Info("Deleted control plane statefulset")

	// Step 4: Wait for workloads to be deleted
	if err := r.waitForWorkloadDeletion(ctx, namespaceName); err != nil {
		log.Info("Waiting for workload deletion, will retry", "error", err.Error())
		// Return error to requeue
		return err
	}
	log.Info("All workloads deleted")

	// Step 5: Delete PVC (this will trigger PV deletion via ownerReference)
	if err := r.deletePVC(ctx, namespaceName, GetPVCName()); err != nil {
		return fmt.Errorf("failed to delete PVC: %w", err)
	}
	log.Info("Deleted PVC")

	// Step 6: Delete PV explicitly
	pvName := GetPVName(cluster.Name)
	if err := r.deletePV(ctx, pvName); err != nil {
		return fmt.Errorf("failed to delete PV: %w", err)
	}
	log.Info("Deleted PV")

	// Step 7: Wait for PV deletion
	if err := r.waitForPVDeletion(ctx, pvName); err != nil {
		log.Info("Waiting for PV deletion, will retry", "error", err.Error())
		return err
	}
	log.Info("PV fully deleted")

	// Step 8: Create new PV on target node
	newPV := BuildPersistentVolumeWithNode(cluster, targetNode)
	if err := r.createOrUpdate(ctx, newPV); err != nil {
		return fmt.Errorf("failed to create PV on target node: %w", err)
	}
	log.Info("Created new PV on target node", "targetNode", targetNode)

	// Step 9: Update cluster status to store target node
	cluster.Status.CurrentNode = targetNode
	cluster.Status.LastRelocationTime = time.Now().Format(time.RFC3339)
	if err := r.Status().Update(ctx, cluster); err != nil {
		log.Error(err, "Failed to update status with target node")
		// Non-fatal, continue
	}

	log.Info("Cluster relocation preparation complete. Resources will be recreated on next reconciliation.", "targetNode", targetNode)

	return nil
}

// deleteDeployment deletes a deployment
func (r *KloneClusterReconciler) deleteDeployment(ctx context.Context, namespace, name string) error {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted
			return nil
		}
		return err
	}

	if err := r.Delete(ctx, deployment); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// deleteStatefulSet deletes a statefulset
func (r *KloneClusterReconciler) deleteStatefulSet(ctx context.Context, namespace, name string) error {
	statefulset := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, statefulset)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted
			return nil
		}
		return err
	}

	if err := r.Delete(ctx, statefulset); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// deletePVC deletes a persistent volume claim
func (r *KloneClusterReconciler) deletePVC(ctx context.Context, namespace, name string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted
			return nil
		}
		return err
	}

	if err := r.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// deletePV deletes a persistent volume
func (r *KloneClusterReconciler) deletePV(ctx context.Context, name string) error {
	pv := &corev1.PersistentVolume{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted
			return nil
		}
		return err
	}

	if err := r.Delete(ctx, pv); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// waitForWorkloadDeletion waits for all workloads to be deleted
func (r *KloneClusterReconciler) waitForWorkloadDeletion(ctx context.Context, namespace string) error {
	// Check if statefulset is deleted
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: GetControlPlaneStatefulSetName(), Namespace: namespace}, sts)
	if err == nil {
		return fmt.Errorf("statefulset %s still exists", GetControlPlaneStatefulSetName())
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Check if worker deployment is deleted
	workerDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: GetWorkerDeploymentName(), Namespace: namespace}, workerDep)
	if err == nil {
		return fmt.Errorf("deployment %s still exists", GetWorkerDeploymentName())
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Check if terminal deployment is deleted
	terminalDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: GetTerminalDeploymentName(), Namespace: namespace}, terminalDep)
	if err == nil {
		return fmt.Errorf("deployment %s still exists", GetTerminalDeploymentName())
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// waitForPVDeletion waits for PV to be fully deleted
func (r *KloneClusterReconciler) waitForPVDeletion(ctx context.Context, pvName string) error {
	pv := &corev1.PersistentVolume{}
	err := r.Get(ctx, types.NamespacedName{Name: pvName}, pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// PV is deleted
			return nil
		}
		return err
	}
	// PV still exists
	return fmt.Errorf("PV %s still exists", pvName)
}

// shouldRelocate checks if relocation is needed based on cluster status
func (r *KloneClusterReconciler) shouldRelocate(_ context.Context, cluster *klonev1alpha1.KloneCluster) bool {
	// Don't relocate if already in progress
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "RelocationInProgress" && condition.Status == metav1.ConditionTrue {
			return false
		}
	}

	// Don't relocate too frequently (at most once every 5 minutes)
	if cluster.Status.LastRelocationTime != "" {
		lastRelocation, err := time.Parse(time.RFC3339, cluster.Status.LastRelocationTime)
		if err == nil {
			if time.Since(lastRelocation) < 5*time.Minute {
				return false
			}
		}
	}

	return true
}

// setRelocationCondition sets the RelocationInProgress condition
func (r *KloneClusterReconciler) setRelocationCondition(ctx context.Context, cluster *klonev1alpha1.KloneCluster, inProgress bool, reason string) error {
	status := metav1.ConditionFalse
	if inProgress {
		status = metav1.ConditionTrue
	}

	condition := metav1.Condition{
		Type:               "RelocationInProgress",
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            reason,
	}

	// Update or add condition
	found := false
	for i, c := range cluster.Status.Conditions {
		if c.Type == "RelocationInProgress" {
			cluster.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		cluster.Status.Conditions = append(cluster.Status.Conditions, condition)
	}

	if inProgress {
		cluster.Status.RelocationReason = reason
	}

	return r.Status().Update(ctx, cluster)
}
