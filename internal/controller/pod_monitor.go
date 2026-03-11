package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
)

// checkTerminalPodSchedulability checks if the terminal pod is unschedulable
// Returns: isUnschedulable bool, reason string, targetNode string, error
func (r *KloneClusterReconciler) checkTerminalPodSchedulability(ctx context.Context, cluster *klonev1alpha1.KloneCluster) (bool, string, string, error) {
	log := logf.FromContext(ctx)
	namespaceName := cluster.Name

	// Get terminal pods
	podList := &corev1.PodList{}
	err := r.List(ctx, podList, client.InNamespace(namespaceName), client.MatchingLabels{
		"app": "klone-terminal",
	})
	if err != nil {
		return false, "", "", fmt.Errorf("failed to list terminal pods: %w", err)
	}

	if len(podList.Items) == 0 {
		// No terminal pod exists yet
		return false, "", "", nil
	}

	// Check each terminal pod
	for _, pod := range podList.Items {
		// If pod is already running or succeeded, no issue
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}

		// Check if pod is pending
		if pod.Status.Phase == corev1.PodPending {
			// Check pod conditions for scheduling failures
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
					reason := condition.Reason
					message := condition.Message

					log.Info("Terminal pod unschedulable",
						"pod", pod.Name,
						"reason", reason,
						"message", message)

					// Check if it's a pod capacity issue or affinity issue
					if isSchedulingFailureDueToNodeConstraints(reason, message) {
						// Find a node with capacity
						targetNode, err := r.findNodeWithCapacity(ctx, cluster)
						if err != nil {
							return true, fmt.Sprintf("%s: %s", reason, message), "", err
						}
						if targetNode == "" {
							return true, fmt.Sprintf("%s: %s", reason, message), "", fmt.Errorf("no suitable node found with capacity")
						}

						return true, fmt.Sprintf("%s: %s", reason, message), targetNode, nil
					}
				}
			}

			// Also check pod events for FailedScheduling
			eventList := &corev1.EventList{}
			err := r.List(ctx, eventList, client.InNamespace(namespaceName))
			if err != nil {
				log.Error(err, "Failed to list events")
				continue
			}

			for _, event := range eventList.Items {
				if event.InvolvedObject.Name == pod.Name &&
					event.InvolvedObject.Kind == "Pod" &&
					event.Reason == "FailedScheduling" {

					log.Info("FailedScheduling event detected for terminal pod",
						"pod", pod.Name,
						"message", event.Message)

					if isSchedulingFailureDueToNodeConstraints(event.Reason, event.Message) {
						targetNode, err := r.findNodeWithCapacity(ctx, cluster)
						if err != nil {
							return true, event.Message, "", err
						}
						if targetNode == "" {
							return true, event.Message, "", fmt.Errorf("no suitable node found with capacity")
						}

						return true, event.Message, targetNode, nil
					}
				}
			}
		}
	}

	return false, "", "", nil
}

// isSchedulingFailureDueToNodeConstraints checks if the scheduling failure is due to node constraints
func isSchedulingFailureDueToNodeConstraints(reason, message string) bool {
	// Check for various node constraint failures
	constraintIndicators := []string{
		"Insufficient pods",
		"PodExceedsFreePods",
		"node(s) didn't match pod affinity rules",
		"node(s) didn't match pod affinity/anti-affinity",
		"didn't match pod affinity rules",
		"no nodes available to schedule pods",
		"Too many pods",
		"pod affinity",
	}

	messageLower := strings.ToLower(message)
	reasonLower := strings.ToLower(reason)

	for _, indicator := range constraintIndicators {
		if strings.Contains(messageLower, strings.ToLower(indicator)) ||
			strings.Contains(reasonLower, strings.ToLower(indicator)) {
			return true
		}
	}

	return false
}

// findNodeWithCapacity finds a node that has capacity for control plane, worker, and terminal pods
func (r *KloneClusterReconciler) findNodeWithCapacity(ctx context.Context, _ *klonev1alpha1.KloneCluster) (string, error) {
	log := logf.FromContext(ctx)

	// List all nodes
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return "", fmt.Errorf("failed to list nodes: %w", err)
	}

	// We need capacity for 3 pods minimum: control-plane, worker, terminal
	requiredPodCapacity := int64(3)

	for _, node := range nodeList.Items {
		// Skip nodes that are not ready
		if !isNodeReady(&node) {
			log.Info("Skipping unready node", "node", node.Name)
			continue
		}

		// Get pod capacity and current pod count on this node
		podCapacity, ok := node.Status.Allocatable[corev1.ResourcePods]
		if !ok {
			continue
		}

		maxPods := podCapacity.Value()

		// Count current pods on this node
		podList := &corev1.PodList{}
		err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name})
		if err != nil {
			log.Error(err, "Failed to list pods on node", "node", node.Name)
			continue
		}

		// Count only non-terminated pods
		currentPods := int64(0)
		for _, pod := range podList.Items {
			if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
				currentPods++
			}
		}

		availableCapacity := maxPods - currentPods

		log.Info("Checking node capacity",
			"node", node.Name,
			"maxPods", maxPods,
			"currentPods", currentPods,
			"available", availableCapacity,
			"required", requiredPodCapacity)

		if availableCapacity >= requiredPodCapacity {
			log.Info("Found node with sufficient capacity", "node", node.Name, "available", availableCapacity)
			return node.Name, nil
		}
	}

	return "", fmt.Errorf("no node found with capacity for %d pods", requiredPodCapacity)
}

// isNodeReady checks if a node is in Ready condition
func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

