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

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
)

// KloneClusterReconciler reconciles a KloneCluster object
type KloneClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// RBAC markers for controller permissions

// +kubebuilder:rbac:groups=klone.klone.io,resources=kloneclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=klone.klone.io,resources=kloneclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=klone.klone.io,resources=kloneclusters/finalizers,verbs=update

// Core resources
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces/finalize,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create;get

// Workloads
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Networking
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop
func (r *KloneClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KloneCluster instance
	cluster := &klonev1alpha1.KloneCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource deleted, no action needed
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KloneCluster")
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer
	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cluster)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cluster, KloneFinalizer) {
		controllerutil.AddFinalizer(cluster, KloneFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Allocate CIDRs if not already set
	if cluster.Status.ClusterCIDR == "" || cluster.Status.ServiceCIDR == "" {
		clusterCIDR, serviceCIDR := AllocateCIDRs(cluster.Name)
		cluster.Status.ClusterCIDR = clusterCIDR
		cluster.Status.ServiceCIDR = serviceCIDR
		cluster.Status.Namespace = GetNamespaceName(cluster.Name)
		cluster.Status.PersistentVolume = GetPVName(cluster.Name)
		cluster.Status.Phase = PhaseCreating

		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "Failed to update status with CIDRs")
			return ctrl.Result{}, err
		}
	}

	// Handle restart annotation
	if err := r.handleRestartAnnotation(ctx, cluster); err != nil {
		log.Error(err, "Failed to handle restart annotation")
	}

	// Reconcile all resources
	if err := r.reconcileResources(ctx, cluster); err != nil {
		log.Error(err, "Failed to reconcile resources")
		// Update status to Failed
		cluster.Status.Phase = PhaseFailed
		_ = r.Status().Update(ctx, cluster)
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, cluster); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Requeue after 30 seconds to check status
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcileResources ensures all cluster resources exist
func (r *KloneClusterReconciler) reconcileResources(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)

	// 1. Reconcile Namespace
	ns := BuildNamespace(cluster)
	if err := r.createOrUpdate(ctx, ns); err != nil {
		return fmt.Errorf("failed to reconcile namespace: %w", err)
	}
	log.Info("Reconciled Namespace", "name", ns.Name)

	// 2. Reconcile PersistentVolume (cluster-scoped, no owner reference)
	pv := BuildPersistentVolume(cluster)
	if err := r.createOrUpdatePV(ctx, pv); err != nil {
		return fmt.Errorf("failed to reconcile PV: %w", err)
	}
	log.Info("Reconciled PersistentVolume", "name", pv.Name)

	// For namespaced resources, set owner reference to enable garbage collection
	namespaceName := GetNamespaceName(cluster.Name)

	// 3. Reconcile PersistentVolumeClaim
	pvc := BuildPersistentVolumeClaim(cluster)
	pvc.SetNamespace(namespaceName)
	if err := r.createOrUpdate(ctx, pvc); err != nil {
		return fmt.Errorf("failed to reconcile PVC: %w", err)
	}
	log.Info("Reconciled PVC", "namespace", namespaceName, "name", pvc.Name)

	// 4. Reconcile Services
	controlPlaneSvc := BuildControlPlaneService(cluster)
	if err := r.createOrUpdate(ctx, controlPlaneSvc); err != nil {
		return fmt.Errorf("failed to reconcile control plane service: %w", err)
	}
	log.Info("Reconciled control plane Service", "namespace", namespaceName, "name", controlPlaneSvc.Name)

	terminalSvc := BuildTerminalService(cluster)
	if err := r.createOrUpdate(ctx, terminalSvc); err != nil {
		return fmt.Errorf("failed to reconcile terminal service: %w", err)
	}
	log.Info("Reconciled terminal Service", "namespace", namespaceName, "name", terminalSvc.Name)

	// 5. Reconcile Control Plane StatefulSet
	controlPlaneSS := BuildControlPlaneStatefulSet(cluster)
	if err := r.createOrUpdate(ctx, controlPlaneSS); err != nil {
		return fmt.Errorf("failed to reconcile control plane: %w", err)
	}
	log.Info("Reconciled control plane StatefulSet", "namespace", namespaceName, "name", controlPlaneSS.Name)

	// 6. Reconcile Worker Deployment
	workerDep := BuildWorkerDeployment(cluster)
	if err := r.createOrUpdate(ctx, workerDep); err != nil {
		return fmt.Errorf("failed to reconcile workers: %w", err)
	}
	log.Info("Reconciled worker Deployment", "namespace", namespaceName, "name", workerDep.Name)

	// 7. Reconcile Terminal Deployment
	terminalDep := BuildTerminalDeployment(cluster)
	if err := r.createOrUpdate(ctx, terminalDep); err != nil {
		return fmt.Errorf("failed to reconcile terminal: %w", err)
	}
	log.Info("Reconciled terminal Deployment", "namespace", namespaceName, "name", terminalDep.Name)

	// 8. Reconcile Ingress (if configured)
	ingress := BuildIngress(cluster)
	if ingress != nil {
		if err := r.createOrUpdate(ctx, ingress); err != nil {
			return fmt.Errorf("failed to reconcile ingress: %w", err)
		}
		log.Info("Reconciled Ingress", "namespace", namespaceName, "name", ingress.Name, "type", cluster.Spec.Ingress.Type)
	}

	return nil
}

// createOrUpdate creates or updates a resource
func (r *KloneClusterReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject().(client.Object)

	err := r.Get(ctx, key, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new resource
			return r.Create(ctx, obj)
		}
		return err
	}

	// Resource exists, update if needed
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// createOrUpdatePV handles PV creation/update (no owner reference for cluster-scoped resources)
func (r *KloneClusterReconciler) createOrUpdatePV(ctx context.Context, pv *corev1.PersistentVolume) error {
	existing := &corev1.PersistentVolume{}
	err := r.Get(ctx, types.NamespacedName{Name: pv.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, pv)
		}
		return err
	}

	// PV exists, update if needed
	pv.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, pv)
}

// updateStatus updates the KloneCluster status based on actual resource state
func (r *KloneClusterReconciler) updateStatus(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)
	namespaceName := GetNamespaceName(cluster.Name)

	workloads := []klonev1alpha1.WorkloadStatus{}

	// Check control plane StatefulSet
	controlPlaneSS := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetControlPlaneStatefulSetName(),
		Namespace: namespaceName,
	}, controlPlaneSS); err == nil {
		workloads = append(workloads, klonev1alpha1.WorkloadStatus{
			Kind:    "StatefulSet",
			Name:    controlPlaneSS.Name,
			Ready:   controlPlaneSS.Status.ReadyReplicas,
			Desired: *controlPlaneSS.Spec.Replicas,
		})
	}

	// Check worker Deployment
	workerDep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetWorkerDeploymentName(),
		Namespace: namespaceName,
	}, workerDep); err == nil {
		workloads = append(workloads, klonev1alpha1.WorkloadStatus{
			Kind:    "Deployment",
			Name:    workerDep.Name,
			Ready:   workerDep.Status.ReadyReplicas,
			Desired: *workerDep.Spec.Replicas,
		})
	}

	// Check terminal Deployment
	terminalDep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      GetTerminalDeploymentName(),
		Namespace: namespaceName,
	}, terminalDep); err == nil {
		workloads = append(workloads, klonev1alpha1.WorkloadStatus{
			Kind:    "Deployment",
			Name:    terminalDep.Name,
			Ready:   terminalDep.Status.ReadyReplicas,
			Desired: *terminalDep.Spec.Replicas,
		})
	}

	cluster.Status.Workloads = workloads

	// Check if all workloads are ready
	allReady := true
	terminalReady := false
	for _, w := range workloads {
		if w.Ready != w.Desired {
			allReady = false
		}
		if w.Name == GetTerminalDeploymentName() && w.Ready == w.Desired && w.Ready > 0 {
			terminalReady = true
		}
	}

	// Update conditions
	readyCondition := metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ResourcesNotReady",
		Message:            "Waiting for all resources to be ready",
		LastTransitionTime: metav1.Now(),
	}

	if allReady {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllResourcesReady"
		readyCondition.Message = "All cluster resources are ready"
		cluster.Status.Phase = PhaseRunning
	}

	terminalReadyCondition := metav1.Condition{
		Type:               ConditionTerminalReady,
		Status:             metav1.ConditionFalse,
		Reason:             "TerminalNotReady",
		Message:            "Terminal pod is not ready",
		LastTransitionTime: metav1.Now(),
	}

	if terminalReady {
		terminalReadyCondition.Status = metav1.ConditionTrue
		terminalReadyCondition.Reason = "TerminalReady"
		terminalReadyCondition.Message = "Terminal pod is ready and accessible"
	}

	// Update ingress URL
	if cluster.Spec.Ingress != nil {
		ingress := &networkingv1.Ingress{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      GetIngressName(),
			Namespace: namespaceName,
		}, ingress); err == nil {
			// Update ingress URL based on type
			if cluster.Spec.Ingress.Type == IngressTypeTailscale {
				if cluster.Spec.Ingress.Tailscale != nil && cluster.Spec.Ingress.Tailscale.Domain != "" {
					cluster.Status.IngressURL = fmt.Sprintf("https://%s-terminal.%s",
						cluster.Name, cluster.Spec.Ingress.Tailscale.Domain)
				}
			} else if cluster.Spec.Ingress.Type == IngressTypeLoadBalancer {
				// Get LB hostname from ingress status
				if len(ingress.Status.LoadBalancer.Ingress) > 0 {
					lbHostname := ingress.Status.LoadBalancer.Ingress[0].Hostname
					if lbHostname != "" {
						cluster.Status.LoadBalancerHostname = lbHostname
						cluster.Status.IngressURL = fmt.Sprintf("https://%s", lbHostname)
					}
				}
			}
		}
	}

	// Update or append conditions
	cluster.Status.Conditions = updateCondition(cluster.Status.Conditions, readyCondition)
	cluster.Status.Conditions = updateCondition(cluster.Status.Conditions, terminalReadyCondition)

	log.Info("Updating status", "phase", cluster.Status.Phase, "workloads", len(workloads))

	return r.Status().Update(ctx, cluster)
}

// updateCondition updates or appends a condition to the condition list
func updateCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	for i, condition := range conditions {
		if condition.Type == newCondition.Type {
			conditions[i] = newCondition
			return conditions
		}
	}
	return append(conditions, newCondition)
}

// handleDeletion handles cluster deletion with finalizer cleanup
func (r *KloneClusterReconciler) handleDeletion(ctx context.Context, cluster *klonev1alpha1.KloneCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(cluster, KloneFinalizer) {
		// Update phase
		cluster.Status.Phase = PhaseTerminating
		_ = r.Status().Update(ctx, cluster)

		log.Info("Cleaning up cluster resources", "cluster", cluster.Name)

		// Clear finalizers from namespace resources
		namespaceName := GetNamespaceName(cluster.Name)
		if err := r.clearNamespaceFinalizers(ctx, namespaceName); err != nil {
			log.Error(err, "Failed to clear namespace finalizers")
		}

		// Delete PersistentVolume
		pv := &corev1.PersistentVolume{}
		pvName := GetPVName(cluster.Name)
		if err := r.Get(ctx, types.NamespacedName{Name: pvName}, pv); err == nil {
			if err := r.Delete(ctx, pv); err != nil {
				log.Error(err, "Failed to delete PV")
			}
		}

		// Delete Namespace
		ns := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: namespaceName}, ns); err == nil {
			if err := r.Delete(ctx, ns); err != nil {
				log.Error(err, "Failed to delete namespace")
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(cluster, KloneFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Cluster cleanup completed", "cluster", cluster.Name)
	}

	return ctrl.Result{}, nil
}

// clearNamespaceFinalizers clears finalizers from resources in the namespace
func (r *KloneClusterReconciler) clearNamespaceFinalizers(ctx context.Context, namespace string) error {
	// Clear finalizers from Ingresses
	ingressList := &networkingv1.IngressList{}
	if err := r.List(ctx, ingressList, client.InNamespace(namespace)); err == nil {
		for _, ingress := range ingressList.Items {
			if len(ingress.Finalizers) > 0 {
				ingress.Finalizers = []string{}
				_ = r.Update(ctx, &ingress)
			}
		}
	}

	// Clear finalizers from Secrets (Tailscale operator)
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(namespace)); err == nil {
		for _, secret := range secretList.Items {
			if len(secret.Finalizers) > 0 {
				secret.Finalizers = []string{}
				_ = r.Update(ctx, &secret)
			}
		}
	}

	// Clear finalizers from StatefulSets
	ssList := &appsv1.StatefulSetList{}
	if err := r.List(ctx, ssList, client.InNamespace(namespace)); err == nil {
		for _, ss := range ssList.Items {
			if len(ss.Finalizers) > 0 {
				ss.Finalizers = []string{}
				_ = r.Update(ctx, &ss)
			}
		}
	}

	// Clear finalizers from Deployments
	depList := &appsv1.DeploymentList{}
	if err := r.List(ctx, depList, client.InNamespace(namespace)); err == nil {
		for _, dep := range depList.Items {
			if len(dep.Finalizers) > 0 {
				dep.Finalizers = []string{}
				_ = r.Update(ctx, &dep)
			}
		}
	}

	// Clear finalizers from PVCs
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList, client.InNamespace(namespace)); err == nil {
		for _, pvc := range pvcList.Items {
			if len(pvc.Finalizers) > 0 {
				pvc.Finalizers = []string{}
				_ = r.Update(ctx, &pvc)
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KloneClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&klonev1alpha1.KloneCluster{}).
		// Watch owned resources
		Owns(&appsv1.StatefulSet{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&networkingv1.Ingress{}).
		Named("klonecluster").
		Complete(r)
}
