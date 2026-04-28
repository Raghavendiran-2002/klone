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
	"maps"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

const (
	// ArgoCDNamespaceDefault is the default namespace for ArgoCD
	ArgoCDNamespaceDefault = "argocd"
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
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces/finalize,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// ^^ includes ArgoCD secrets for cluster registration and credentials secrets
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create;get;list
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch

// Workloads
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Networking
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// RBAC for ArgoCD integration
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apiregistration.k8s.io,resources=apiservices,verbs=get;list;watch;create;update;patch

// Metrics API for dashboard
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list

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
	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cluster)
	}

	// Add finalizer if not present — use Patch to avoid resource-version conflicts
	if !controllerutil.ContainsFinalizer(cluster, KloneFinalizer) {
		patch := client.MergeFrom(cluster.DeepCopy())
		controllerutil.AddFinalizer(cluster, KloneFinalizer)
		if err := r.Patch(ctx, cluster, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Allocate CIDRs if not already set
	if cluster.Status.ClusterCIDR == "" || cluster.Status.ServiceCIDR == "" {
		statusPatch := client.MergeFrom(cluster.DeepCopy())
		clusterCIDR, serviceCIDR := AllocateCIDRs(cluster.Name)
		cluster.Status.ClusterCIDR = clusterCIDR
		cluster.Status.ServiceCIDR = serviceCIDR
		cluster.Status.Namespace = GetNamespaceName(cluster.Name)
		cluster.Status.PersistentVolume = GetPVName(cluster.Name)
		cluster.Status.Phase = PhaseCreating

		if err := r.Status().Patch(ctx, cluster, statusPatch); err != nil {
			log.Error(err, "Failed to patch status with CIDRs")
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
		// Patch status to Failed — best-effort, original error is still returned
		statusPatch := client.MergeFrom(cluster.DeepCopy())
		cluster.Status.Phase = PhaseFailed
		if patchErr := r.Status().Patch(ctx, cluster, statusPatch); patchErr != nil {
			log.Error(patchErr, "Failed to patch status to Failed")
		}
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
//
//nolint:gocyclo // Complex reconciliation logic required
func (r *KloneClusterReconciler) reconcileResources(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)

	// 1. Reconcile Namespace
	ns := BuildNamespace(cluster)
	if err := r.createOrUpdate(ctx, ns); err != nil {
		return fmt.Errorf("failed to reconcile namespace: %w", err)
	}
	log.Info("Reconciled Namespace", "name", ns.Name)

	// 2. Reconcile Credentials Secret
	if cluster.Status.CredentialsSecretName == "" {
		// Determine username: use spec.username if provided, otherwise use cluster name
		username := cluster.Spec.Username
		if username == "" {
			username = cluster.Name
		}

		// Generate random password
		password, err := GenerateRandomPassword(16)
		if err != nil {
			return fmt.Errorf("failed to generate password: %w", err)
		}

		// Create credentials secret
		credSecret := BuildCredentialsSecret(cluster, username, password)
		if err := r.createOrUpdate(ctx, credSecret); err != nil {
			return fmt.Errorf("failed to create credentials secret: %w", err)
		}
		log.Info("Created credentials secret", "name", credSecret.Name, "username", username)

		// Update status with secret name
		cluster.Status.CredentialsSecretName = credSecret.Name
		if err := r.Status().Update(ctx, cluster); err != nil {
			return fmt.Errorf("failed to update status with credentials secret name: %w", err)
		}
	}

	// 3. Reconcile PersistentVolume (cluster-scoped, no owner reference)
	pv := BuildPersistentVolume(cluster)
	if err := r.createOrUpdatePV(ctx, pv); err != nil {
		return fmt.Errorf("failed to reconcile PV: %w", err)
	}

	// If the PV is still terminating (relocation in progress), do not recreate
	// the PVC or workloads — let the deletion complete before proceeding.
	existingPV := &corev1.PersistentVolume{}
	if err := r.Get(ctx, types.NamespacedName{Name: pv.Name}, existingPV); err == nil {
		if existingPV.DeletionTimestamp != nil {
			log.Info("PV deletion is in progress, skipping workload reconciliation until PV is fully removed", "pv", pv.Name)
			return nil
		}
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

	// 7. Check if control plane and workers are ready before deploying terminal
	controlPlaneReady, err := r.isStatefulSetReady(ctx, namespaceName, GetControlPlaneStatefulSetName())
	if err != nil {
		log.Info("Control plane not ready, requeueing", "error", err)
		return nil
	}

	workerReady, err := r.isDeploymentReady(ctx, namespaceName, GetWorkerDeploymentName())
	if err != nil {
		log.Info("Workers not ready, requeueing", "error", err)
		return nil
	}

	// 7a. If the worker is not ready, check whether it is stuck due to pod-affinity
	// conflicts (control-plane landed on a full node).  Trigger relocation early
	// so we don't wait for the terminal to be created first.
	if !workerReady {
		isUnschedulable, reason, targetNode, err := r.checkWorkerPodSchedulability(ctx, cluster)
		if err != nil {
			log.Error(err, "Failed to check worker pod schedulability")
		} else if isUnschedulable {
			log.Info("Worker pod is unschedulable, triggering relocation",
				"reason", reason, "targetNode", targetNode)
			if r.shouldRelocate(ctx, cluster) {
				if err := r.setRelocationCondition(ctx, cluster, true, reason); err != nil {
					log.Error(err, "Failed to set relocation condition")
				}
				if err := r.relocateClusterToNode(ctx, cluster, targetNode); err != nil {
					log.Error(err, "Failed to relocate cluster", "targetNode", targetNode)
					if err2 := r.setRelocationCondition(ctx, cluster, false, fmt.Sprintf("Relocation failed: %v", err)); err2 != nil {
						log.Error(err2, "Failed to clear relocation condition")
					}
					return fmt.Errorf("relocation failed: %w", err)
				}
				if err := r.setRelocationCondition(ctx, cluster, false, "Relocation completed successfully"); err != nil {
					log.Error(err, "Failed to clear relocation condition")
				}
				log.Info("Cluster relocation completed successfully", "targetNode", targetNode)
				return nil
			}
			log.Info("Worker relocation skipped (already in progress or too frequent)")
		}
	}

	// Only deploy terminal if control plane and workers are ready
	if controlPlaneReady && workerReady {
		log.Info("Control plane and workers ready, reconciling terminal")
		terminalDep := BuildTerminalDeployment(cluster)
		if err := r.createOrUpdate(ctx, terminalDep); err != nil {
			return fmt.Errorf("failed to reconcile terminal: %w", err)
		}
		log.Info("Reconciled terminal Deployment", "namespace", namespaceName, "name", terminalDep.Name)

		// 7b. Check if terminal pod is schedulable and trigger relocation if needed
		isUnschedulable, reason, targetNode, err := r.checkTerminalPodSchedulability(ctx, cluster)
		if err != nil {
			log.Error(err, "Failed to check terminal pod schedulability")
		} else if isUnschedulable {
			log.Info("Terminal pod is unschedulable", "reason", reason, "targetNode", targetNode)

			if r.shouldRelocate(ctx, cluster) {
				log.Info("Relocating cluster to resolve terminal pod scheduling issue",
					"targetNode", targetNode, "reason", reason)

				if err := r.setRelocationCondition(ctx, cluster, true, reason); err != nil {
					log.Error(err, "Failed to set relocation condition")
				}

				if err := r.relocateClusterToNode(ctx, cluster, targetNode); err != nil {
					log.Error(err, "Failed to relocate cluster", "targetNode", targetNode)
					if err := r.setRelocationCondition(ctx, cluster, false, fmt.Sprintf("Relocation failed: %v", err)); err != nil {
						log.Error(err, "Failed to clear relocation condition")
					}
					return fmt.Errorf("relocation failed: %w", err)
				}

				if err := r.setRelocationCondition(ctx, cluster, false, "Relocation completed successfully"); err != nil {
					log.Error(err, "Failed to clear relocation condition")
				}

				log.Info("Cluster relocation completed successfully", "targetNode", targetNode)
				return nil
			}
			log.Info("Relocation skipped (already in progress or too frequent)")
		}
	} else {
		log.Info("Terminal deployment deferred, control plane or workers not ready",
			"controlPlaneReady", controlPlaneReady, "workerReady", workerReady)
	}

	// 8. Ingress removed - Dashboard accesses terminal service directly
	// Terminal service (klone-terminal) is still created above at step 4

	// 9. Ensure host metrics-server is installed (run once, K3s clusters have it built-in)
	if err := r.EnsureHostMetricsServer(ctx); err != nil {
		log.Error(err, "Failed to ensure host metrics-server is installed")
	}

	// 10. Install ArgoCD CRDs in nested K3s cluster by default (if terminal is ready)
	// DISABLED: ArgoCD installation via Helm removed - only keeping registration functionality
	/*
		terminalReady, err := r.isDeploymentReady(ctx, namespaceName, GetTerminalDeploymentName())
		if err == nil && terminalReady && !cluster.Status.ArgoCDCRDsInstalled {
			log.Info("Terminal is ready, installing ArgoCD CRDs in nested K3s cluster")

			// Create RBAC resources for ArgoCD CRD installation job
			crdSA := BuildArgoCDCRDServiceAccount(cluster)
			if err := r.createOrUpdate(ctx, crdSA); err != nil {
				log.Error(err, "Failed to reconcile ArgoCD CRD ServiceAccount")
			} else {
				log.Info("Reconciled ArgoCD CRD ServiceAccount", "namespace", namespaceName, "name", crdSA.Name)
			}

			crdRole := BuildArgoCDCRDRole(cluster)
			if err := r.createOrUpdate(ctx, crdRole); err != nil {
				log.Error(err, "Failed to reconcile ArgoCD CRD Role")
			} else {
				log.Info("Reconciled ArgoCD CRD Role", "namespace", namespaceName, "name", crdRole.Name)
			}

			crdRB := BuildArgoCDCRDRoleBinding(cluster)
			if err := r.createOrUpdate(ctx, crdRB); err != nil {
				log.Error(err, "Failed to reconcile ArgoCD CRD RoleBinding")
			} else {
				log.Info("Reconciled ArgoCD CRD RoleBinding", "namespace", namespaceName, "name", crdRB.Name)
			}

			// Create RBAC resources for reading secrets from argocd namespace
			secretReaderRole := BuildArgoCDSecretReaderRole(cluster)
			if err := r.createOrUpdate(ctx, secretReaderRole); err != nil {
				log.Error(err, "Failed to reconcile ArgoCD Secret Reader Role")
			} else {
				log.Info("Reconciled ArgoCD Secret Reader Role", "namespace", secretReaderRole.Namespace, "name", secretReaderRole.Name)
			}

			secretReaderRB := BuildArgoCDSecretReaderRoleBinding(cluster)
			if err := r.createOrUpdate(ctx, secretReaderRB); err != nil {
				log.Error(err, "Failed to reconcile ArgoCD Secret Reader RoleBinding")
			} else {
				log.Info("Reconciled ArgoCD Secret Reader RoleBinding", "namespace", secretReaderRB.Namespace, "name", secretReaderRB.Name)
			}

			// Check if CRD installation job already exists
			argoCDCRDJob := BuildArgoCDCRDInstallJob(cluster)
			existingCRDJob := &batchv1.Job{}
			crdJobKey := client.ObjectKey{Namespace: namespaceName, Name: argoCDCRDJob.Name}
			err := r.Get(ctx, crdJobKey, existingCRDJob)

			if err != nil && apierrors.IsNotFound(err) {
				// Create the job
				if err := r.Create(ctx, argoCDCRDJob); err != nil && !apierrors.IsAlreadyExists(err) {
					log.Error(err, "Failed to create ArgoCD CRD installation job")
				} else {
					log.Info("Created ArgoCD CRD installation job", "namespace", namespaceName, "name", argoCDCRDJob.Name)
				}
			} else if err == nil {
				// Job exists, check status
				if existingCRDJob.Status.Succeeded > 0 {
					cluster.Status.ArgoCDCRDsInstalled = true
					log.Info("ArgoCD CRDs installation completed successfully")
				} else if existingCRDJob.Status.Failed > 0 {
					log.Info("ArgoCD CRDs installation failed, will retry on next reconcile")
				} else {
					log.V(1).Info("ArgoCD CRDs installation job is running")
				}
			}
		}
	*/

	// 11. Register with ArgoCD (if enabled and not already registered)
	// Only attempt registration if terminal is ready (needed for kubeconfig extraction)
	terminalReady, err := r.isDeploymentReady(ctx, namespaceName, GetTerminalDeploymentName())
	if err == nil && terminalReady {
		shouldRegister, argoCDNamespace, err := ShouldRegisterWithArgoCD(ctx, r.Client, cluster)
		if err != nil {
			log.Error(err, "Failed to determine if should register with ArgoCD")
		} else if shouldRegister && !cluster.Status.ArgoCDRegistered {
			// If the job permanently failed (BackoffLimitExceeded), stop creating new jobs.
			// The user can clear the annotation to retry.
			const failedAnnotation = "klone.io/argocd-registration-failed"
			permFailed := cluster.Annotations != nil && cluster.Annotations[failedAnnotation] != ""
			if permFailed {
				log.Info("ArgoCD registration permanently failed — clear annotation to retry",
					"failedAt", cluster.Annotations[failedAnnotation])
			} else {
				log.Info("Registering cluster with ArgoCD", "argocdNamespace", argoCDNamespace)

				// Create ServiceAccount, Role, and RoleBinding for ArgoCD registration
				sa := BuildArgoCDServiceAccount(cluster)
				if err := r.createOrUpdate(ctx, sa); err != nil {
					log.Error(err, "Failed to reconcile ArgoCD ServiceAccount")
				} else {
					log.Info("Reconciled ArgoCD ServiceAccount", "namespace", namespaceName, "name", sa.Name)
				}

				role := BuildArgoCDRole(cluster)
				if err := r.createOrUpdate(ctx, role); err != nil {
					log.Error(err, "Failed to reconcile ArgoCD Role")
				} else {
					log.Info("Reconciled ArgoCD Role", "namespace", namespaceName, "name", role.Name)
				}

				rb := BuildArgoCDRoleBinding(cluster)
				if err := r.createOrUpdate(ctx, rb); err != nil {
					log.Error(err, "Failed to reconcile ArgoCD RoleBinding")
				} else {
					log.Info("Reconciled ArgoCD RoleBinding", "namespace", namespaceName, "name", rb.Name)
				}

				// Get ArgoCD credentials
				detector := NewArgoCDDetector(r.Client, argoCDNamespace)
				username, password, err := detector.GetArgoCDCredentials(ctx)
				if err != nil {
					log.Error(err, "Failed to get ArgoCD credentials")
				} else {
					// Create ArgoCD registration Job
					job := BuildArgoCDRegisterJob(cluster, argoCDNamespace, username, password)
					job.SetNamespace(namespaceName)

					// Check if job already exists
					existingJob := &batchv1.Job{}
					jobKey := client.ObjectKey{Namespace: namespaceName, Name: job.Name}
					err := r.Get(ctx, jobKey, existingJob)
					if err != nil && apierrors.IsNotFound(err) {
						if err := r.Create(ctx, job); err != nil {
							log.Error(err, "Failed to create ArgoCD registration job")
						} else {
							log.Info("Created ArgoCD registration job", "namespace", namespaceName, "name", job.Name)
						}
					} else if err == nil {
						if existingJob.Status.Succeeded > 0 {
							cluster.Status.ArgoCDRegistered = true
							cluster.Status.ArgoCDClusterName = GetClusterRegistrationName(cluster)
							log.Info("ArgoCD registration job completed successfully")
						} else {
							// Detect BackoffLimitExceeded — stop recreating indefinitely
							for _, cond := range existingJob.Status.Conditions {
								if cond.Type == batchv1.JobFailed &&
									cond.Status == corev1.ConditionTrue &&
									cond.Reason == "BackoffLimitExceeded" {
									log.Info("ArgoCD registration job permanently failed (BackoffLimitExceeded) — setting annotation to stop retrying. Remove annotation klone.io/argocd-registration-failed to retry.")
									if cluster.Annotations == nil {
										cluster.Annotations = make(map[string]string)
									}
									cluster.Annotations[failedAnnotation] = metav1.Now().UTC().Format(time.RFC3339)
									if updateErr := r.Update(ctx, cluster); updateErr != nil {
										log.Error(updateErr, "Failed to set argocd-registration-failed annotation")
									}
									break
								}
							}
							if existingJob.Status.Failed > 0 {
								log.Info("ArgoCD registration job is retrying", "failed", existingJob.Status.Failed)
							}
						}
					}
				}
			} // end permFailed else
		} else if shouldRegister && cluster.Status.ArgoCDRegistered {
			log.V(1).Info("Cluster already registered with ArgoCD", "clusterName", cluster.Status.ArgoCDClusterName)
		}
	}

	return nil
}

// createOrUpdate ensures a resource exists and is up to date. On creation it
// uses r.Create. On update it applies only intentional field changes via a
// merge patch computed against the live server state. This avoids sending null
// for server-managed fields (uid, creationTimestamp, spec.finalizers on Namespace,
// clusterIP on Service, etc.) which would be rejected by the API server.
func (r *KloneClusterReconciler) createOrUpdate(ctx context.Context, desired client.Object) error {
	key := client.ObjectKeyFromObject(desired)
	existing := desired.DeepCopyObject().(client.Object)

	if err := r.Get(ctx, key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// Snapshot the live state as the merge-patch base (before mutations).
	base := existing.DeepCopyObject().(client.Object)

	// Propagate desired labels and annotations into existing (additive merge).
	applyMetadata(existing, desired)

	// Copy only the spec fields this controller owns. Fields NOT listed here
	// (server-managed ones such as spec.finalizers on Namespace, clusterIP on
	// Service) remain in both base and existing unchanged, so they never appear
	// in the computed patch body.
	switch d := desired.(type) {
	case *corev1.Service:
		e := existing.(*corev1.Service)
		e.Spec.Ports = d.Spec.Ports
		e.Spec.Selector = d.Spec.Selector
		e.Spec.Type = d.Spec.Type
	case *appsv1.StatefulSet:
		existing.(*appsv1.StatefulSet).Spec = d.Spec
	case *appsv1.Deployment:
		existing.(*appsv1.Deployment).Spec = d.Spec
	case *corev1.Secret:
		e := existing.(*corev1.Secret)
		e.Data = d.Data
		e.StringData = d.StringData
		e.Type = d.Type
	case *rbacv1.Role:
		existing.(*rbacv1.Role).Rules = d.Rules
	case *rbacv1.RoleBinding:
		existing.(*rbacv1.RoleBinding).Subjects = d.Subjects
		// RoleRef is immutable after creation — do not copy.
	case *corev1.ServiceAccount, *corev1.PersistentVolumeClaim, *corev1.Namespace:
		// No mutable spec fields managed by this controller;
		// label/annotation propagation above is sufficient.
	}

	return r.Patch(ctx, existing, client.MergeFrom(base))
}

// applyMetadata merges labels and annotations from desired into existing (additive).
func applyMetadata(existing, desired client.Object) {
	labels := existing.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	maps.Copy(labels, desired.GetLabels())
	existing.SetLabels(labels)

	annotations := existing.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	maps.Copy(annotations, desired.GetAnnotations())
	existing.SetAnnotations(annotations)
}

// createOrUpdatePV handles PV creation/update (no owner reference for cluster-scoped resources)
func (r *KloneClusterReconciler) createOrUpdatePV(ctx context.Context, pv *corev1.PersistentVolume) error {
	existing := &corev1.PersistentVolume{}
	existing.Name = pv.Name // Set name before CreateOrPatch

	// Use CreateOrPatch to handle conflicts automatically with retries
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, existing, func() error {
		// If the PV is being deleted (relocation in progress), do not touch it —
		// updating a terminating PV can interfere with the pv-protection finalizer
		// removal and stall the deletion indefinitely.
		if existing.DeletionTimestamp != nil {
			return nil
		}

		// Only update certain fields to avoid conflicts with PV controller
		// Don't update if PV is already bound to avoid disrupting existing bindings
		if existing.Status.Phase == corev1.VolumeBound {
			return nil
		}

		// Copy spec fields
		existing.Spec = pv.Spec
		existing.Labels = pv.Labels
		existing.Annotations = pv.Annotations

		return nil
	})

	return err
}

// isStatefulSetReady checks if a StatefulSet has all replicas ready
func (r *KloneClusterReconciler) isStatefulSetReady(ctx context.Context, namespace, name string) (bool, error) {
	ss := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, ss); err != nil {
		return false, err
	}

	// Check if replicas are defined and ready
	if ss.Spec.Replicas == nil {
		return false, fmt.Errorf("replicas not defined")
	}

	return ss.Status.ReadyReplicas == *ss.Spec.Replicas && *ss.Spec.Replicas > 0, nil
}

// isDeploymentReady checks if a Deployment has all replicas ready
func (r *KloneClusterReconciler) isDeploymentReady(ctx context.Context, namespace, name string) (bool, error) {
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if replicas are defined and ready
	if dep.Spec.Replicas == nil {
		return false, fmt.Errorf("replicas not defined")
	}

	return dep.Status.ReadyReplicas == *dep.Spec.Replicas, nil
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

	// Update conditions — ObservedGeneration lets consumers distinguish stale from current conditions
	readyCondition := metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ResourcesNotReady",
		Message:            "Waiting for all resources to be ready",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: cluster.Generation,
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
		ObservedGeneration: cluster.Generation,
	}

	if terminalReady {
		terminalReadyCondition.Status = metav1.ConditionTrue
		terminalReadyCondition.Reason = "TerminalReady"
		terminalReadyCondition.Message = "Terminal pod is ready and accessible"
	}

	// Ingress URL removed - Dashboard accesses terminal service directly via K8s service
	// No need to track external ingress URLs anymore

	// Only write status when something actually changed — prevents the tight
	// re-reconcile loop caused by status.Update triggering a For() watch event.
	dirty := false
	var changed bool
	cluster.Status.Conditions, changed = updateCondition(cluster.Status.Conditions, readyCondition)
	dirty = dirty || changed
	cluster.Status.Conditions, changed = updateCondition(cluster.Status.Conditions, terminalReadyCondition)
	dirty = dirty || changed

	// Also dirty if workload counts or phase changed
	if len(cluster.Status.Workloads) != len(workloads) {
		dirty = true
	} else {
		for i, w := range workloads {
			if i >= len(cluster.Status.Workloads) ||
				cluster.Status.Workloads[i].Ready != w.Ready ||
				cluster.Status.Workloads[i].Desired != w.Desired {
				dirty = true
				break
			}
		}
	}

	cluster.Status.Workloads = workloads

	if !dirty {
		return nil
	}

	log.Info("Updating status", "phase", cluster.Status.Phase, "workloads", len(workloads))
	return r.Status().Update(ctx, cluster)
}

// updateCondition updates or appends a condition to the condition list.
// LastTransitionTime is only updated when the Status field actually changes,
// preventing spurious status writes on every reconcile.
func updateCondition(conditions []metav1.Condition, newCondition metav1.Condition) ([]metav1.Condition, bool) {
	for i, condition := range conditions {
		if condition.Type == newCondition.Type {
			// Preserve the transition time when status hasn't changed
			if condition.Status == newCondition.Status {
				newCondition.LastTransitionTime = condition.LastTransitionTime
			}
			// Detect if anything meaningful changed
			changed := condition.Status != newCondition.Status ||
				condition.Reason != newCondition.Reason ||
				condition.Message != newCondition.Message
			conditions[i] = newCondition
			return conditions, changed
		}
	}
	// Condition is new
	return append(conditions, newCondition), true
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

		// Unregister from ArgoCD if registered
		if cluster.Status.ArgoCDRegistered {
			if err := r.unregisterFromArgoCD(ctx, cluster); err != nil {
				log.Error(err, "Failed to unregister cluster from ArgoCD")
			}
		}

		// Clean up hostPath directory before deleting PV
		if err := r.cleanupHostPath(ctx, cluster); err != nil {
			log.Error(err, "Failed to cleanup hostPath directory")
		}

		// Delete ArgoCD secret reader RBAC resources in argocd namespace
		// DISABLED: Secret importing functionality removed
		/*
			argoCDNamespace := ArgoCDNamespaceDefault
			if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
				argoCDNamespace = cluster.Spec.ArgoCD.Namespace
			}

			secretReaderRBName := fmt.Sprintf("argocd-secret-reader-%s", cluster.Name)
			secretReaderRB := &rbacv1.RoleBinding{}
			if err := r.Get(ctx, types.NamespacedName{Name: secretReaderRBName, Namespace: argoCDNamespace}, secretReaderRB); err == nil {
				if err := r.Delete(ctx, secretReaderRB); err != nil {
					log.Error(err, "Failed to delete ArgoCD secret reader RoleBinding", "namespace", argoCDNamespace)
				} else {
					log.Info("Deleted ArgoCD secret reader RoleBinding", "namespace", argoCDNamespace, "name", secretReaderRBName)
				}
			}

			secretReaderRole := &rbacv1.Role{}
			if err := r.Get(ctx, types.NamespacedName{Name: secretReaderRBName, Namespace: argoCDNamespace}, secretReaderRole); err == nil {
				if err := r.Delete(ctx, secretReaderRole); err != nil {
					log.Error(err, "Failed to delete ArgoCD secret reader Role", "namespace", argoCDNamespace)
				} else {
					log.Info("Deleted ArgoCD secret reader Role", "namespace", argoCDNamespace, "name", secretReaderRBName)
				}
			}
		*/

		// Delete Credentials Secret
		if cluster.Status.CredentialsSecretName != "" {
			credSecret := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      cluster.Status.CredentialsSecretName,
				Namespace: "default",
			}, credSecret); err == nil {
				if err := r.Delete(ctx, credSecret); err != nil {
					log.Error(err, "Failed to delete credentials secret")
				} else {
					log.Info("Deleted credentials secret", "name", cluster.Status.CredentialsSecretName)
				}
			}
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

// cleanupHostPath cleans up the hostPath directory for the cluster
func (r *KloneClusterReconciler) cleanupHostPath(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)

	// Get storage configuration
	hostPathBase := "/tmp/klone"
	if cluster.Spec.Storage.HostPath != "" {
		hostPathBase = cluster.Spec.Storage.HostPath
	}
	hostPath := fmt.Sprintf("%s/%s", hostPathBase, cluster.Name)

	// Get node selector for cleanup job (match the PV node affinity)
	nodeSelector := map[string]string{}
	if cluster.Spec.Storage.NodeAffinity != nil && cluster.Spec.Storage.NodeAffinity.Enabled {
		label := cluster.Spec.Storage.NodeAffinity.Label
		if label == "" {
			label = "primary"
		}
		nodeSelector["workload"] = label
	}

	log.Info("Creating cleanup job for hostPath", "path", hostPath)

	// Create cleanup job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cleanup-%s", cluster.Name),
			Namespace: "klone",
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: int32Ptr(10),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector:  nodeSelector,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "cleanup",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf("rm -rf /host%s && echo 'Cleanup complete for %s'", hostPath, hostPath),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "host-root",
									MountPath: "/host",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "host-root",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/",
								},
							},
						},
					},
				},
			},
		},
	}

	// Create the job
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create cleanup job: %w", err)
	}

	// Wait for job to complete (with timeout)
	for range 30 {
		time.Sleep(1 * time.Second)

		if err := r.Get(ctx, types.NamespacedName{
			Name:      job.Name,
			Namespace: job.Namespace,
		}, job); err != nil {
			continue
		}

		if job.Status.Succeeded > 0 {
			log.Info("Cleanup job completed successfully", "path", hostPath)
			return nil
		}

		if job.Status.Failed > 0 {
			log.Error(nil, "Cleanup job failed", "path", hostPath)
			return fmt.Errorf("cleanup job failed")
		}
	}

	log.Info("Cleanup job timed out, continuing with deletion", "path", hostPath)
	return nil
}

// unregisterFromArgoCD removes the cluster from ArgoCD's managed clusters
func (r *KloneClusterReconciler) unregisterFromArgoCD(ctx context.Context, cluster *klonev1alpha1.KloneCluster) error {
	log := logf.FromContext(ctx)

	// Determine ArgoCD namespace
	argoCDNamespace := ArgoCDNamespaceDefault
	if cluster.Spec.ArgoCD != nil && cluster.Spec.ArgoCD.Namespace != "" {
		argoCDNamespace = cluster.Spec.ArgoCD.Namespace
	}

	// Get ArgoCD credentials
	detector := NewArgoCDDetector(r.Client, argoCDNamespace)
	username, password, err := detector.GetArgoCDCredentials(ctx)
	if err != nil {
		log.Error(err, "Failed to get ArgoCD credentials for unregistration")
		return err
	}

	// Get cluster name
	clusterName := cluster.Status.ArgoCDClusterName
	if clusterName == "" {
		clusterName = GetClusterRegistrationName(cluster)
	}

	// Create a job to unregister the cluster
	namespaceName := GetNamespaceName(cluster.Name)
	jobName := fmt.Sprintf("argocd-unregister-%s", cluster.Name)
	ttl := int32(60) // Clean up after 1 minute
	backoffLimit := int32(2)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespaceName,
			Labels: map[string]string{
				"app":           "argocd-unregister",
				"klone-cluster": cluster.Name,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "argocd-unregister",
							Image:   "quay.io/argoproj/argocd:v2.9.3",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
set -e

# Login to ArgoCD
echo "Logging in to ArgoCD..."
argocd login argocd-server.%s.svc.cluster.local:443 \
  --username=%s \
  --password=%s \
  --insecure

# Unregister the cluster by name
echo "Unregistering cluster: %s"
if argocd cluster list | grep -q "%s"; then
  argocd cluster rm %s --yes || true
  echo "Cluster unregistered successfully"
else
  echo "Cluster not found in ArgoCD, skipping unregistration"
fi
`, argoCDNamespace, username, password, clusterName, clusterName, clusterName),
							},
						},
					},
				},
			},
		},
	}

	// Create the job
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Error(err, "Failed to create ArgoCD unregistration job")
		return err
	}

	log.Info("Created ArgoCD unregistration job", "namespace", namespaceName, "name", jobName)
	return nil
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
	// Set up field indexer for listing pods by node name
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.nodeName", func(obj client.Object) []string {
		pod := obj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

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
