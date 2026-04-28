package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// CleanupController runs background cleanup tasks
type CleanupController struct {
	client.Client
}

// SetupCleanupController adds the cleanup controller to the manager.
// CleanupController implements manager.Runnable so the manager controls its lifecycle.
func SetupCleanupController(mgr manager.Manager) error {
	c := &CleanupController{
		Client: mgr.GetClient(),
	}
	return mgr.Add(c)
}

// Start implements manager.Runnable. It runs the periodic cleanup loop until ctx is cancelled.
func (c *CleanupController) Start(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log := logf.Log.WithName("cleanup-controller")

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.cleanupTerminatingNamespaces(ctx); err != nil {
				log.Error(err, "Failed to clean up terminating namespaces")
			}
		}
	}
}

// cleanupTerminatingNamespaces handles namespaces stuck in Terminating state
func (c *CleanupController) cleanupTerminatingNamespaces(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("cleanup")

	// List all namespaces with klone-managed label
	namespaceList := &corev1.NamespaceList{}
	if err := c.List(ctx, namespaceList, client.MatchingLabels{
		KloneManagedLabel: KloneManagedValue,
	}); err != nil {
		return err
	}

	for _, ns := range namespaceList.Items {
		// Check if namespace is in Terminating state
		if ns.Status.Phase != corev1.NamespaceTerminating {
			continue
		}

		// Check how long it's been terminating
		if ns.DeletionTimestamp == nil {
			continue
		}

		terminatingDuration := time.Since(ns.DeletionTimestamp.Time)
		if terminatingDuration < 2*time.Minute {
			// Give it some time before intervening
			continue
		}

		log.Info("Namespace stuck in Terminating state, clearing finalizers",
			"namespace", ns.Name,
			"terminatingFor", terminatingDuration.String())

		// Re-clear finalizers
		if err := c.clearNamespaceFinalizers(ctx, ns.Name); err != nil {
			log.Error(err, "Failed to clear finalizers", "namespace", ns.Name)
		}

		// Try to patch namespace finalizers
		if len(ns.Finalizers) > 0 {
			nsCopy := ns.DeepCopy()
			nsCopy.Finalizers = []string{}
			if err := c.Update(ctx, nsCopy); err != nil {
				log.Error(err, "Failed to clear namespace finalizers", "namespace", ns.Name)
			} else {
				log.Info("Cleared namespace finalizers", "namespace", ns.Name)
			}
		}
	}

	return nil
}

// clearNamespaceFinalizers is similar to the one in main controller
func (c *CleanupController) clearNamespaceFinalizers(ctx context.Context, namespace string) error {
	r := &KloneClusterReconciler{Client: c.Client}
	return r.clearNamespaceFinalizers(ctx, namespace)
}
