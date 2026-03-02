# Klone Operator Implementation Progress

## Overview
Converting Klone from Python FastAPI to Kubernetes Operator using Kubebuilder.

## Completed Tasks ✅

### Phase 1: Project Setup
- [x] Install Kubebuilder v4.13.0
- [x] Initialize operator project structure
- [x] Set up Go module and dependencies

### Phase 2: CRD Definition
- [x] Define KloneClusterSpec with all fields:
  - K3s configuration (image, token, control plane, workers)
  - Storage configuration (class, size, hostPath, nodeAffinity)
  - Terminal configuration
  - Ingress configuration (tailscale/loadbalancer/none)
  - Networking configuration (auto CIDR allocation)
  - Metrics-server auto-install
- [x] Define KloneClusterStatus:
  - Phase tracking
  - Conditions (Ready, TerminalReady, IngressReady)
  - Workload status
  - Ingress URL and LB hostname
  - Namespace and PV references
- [x] Generate CRD manifests
- [x] Create sample CRs (Tailscale and AWS ALB examples)
- [x] Create utility functions (CIDR allocation, resource naming)

## In Progress 🔨

### Phase 3: Controller Implementation
- [x] Resource builders created
- [x] Main reconciliation loop implemented:
  - [x] Resource creation/update logic
  - [x] CIDR allocation on first reconcile
  - [x] Finalizer handling (add on create)
  - [x] Requeue every 30s for status checks
- [x] Finalizer and cleanup logic:
  - [x] Clear finalizers from Ingresses, Secrets, StatefulSets, Deployments, PVCs
  - [x] Delete PV and Namespace
  - [x] Handle Tailscale operator finalizer conflicts
- [x] Status update logic:
  - [x] Track workload readiness (control-plane, workers, terminal)
  - [x] Update conditions (Ready, TerminalReady)
  - [x] Set phase (Creating, Running, Terminating, Failed)
  - [x] Update ingress URL based on type
- [x] Comprehensive RBAC markers for all resources

### Phase 4: Advanced Features (Completed)
- [x] Restart annotation support:
  - Annotation: `klone.io/restart-requested`
  - Restarts all workloads (control-plane, workers, terminal)
  - Tracks applied restarts to avoid loops
- [x] Background cleanup controller:
  - Runs every 30s
  - Cleans up stuck Terminating namespaces
  - Re-clears finalizers for namespaces stuck > 2 minutes
  - Handles Tailscale operator finalizer conflicts

### Phase 5: Testing & Deployment
- [x] Generate manifests and RBAC
- [x] Build operator binary
- [x] Build and push Docker image (v1.0.0)
- [x] Deploy to cluster (operator-system namespace)
- [x] Create test KloneCluster CR
- [ ] Fix terminal pod issue (ttyd download)
- [ ] Rebuild and redeploy (v1.0.1)
- [ ] Verify end-to-end functionality
- [ ] Test cluster deletion and finalizer cleanup

## Pending Tasks 📋

### Phase 4: Advanced Features
- [ ] Metrics-server auto-install (Job-based)
- [ ] Background cleanup controller for stuck namespaces
- [ ] Nested cluster metrics collection

### Phase 5: Dashboard Service
- [ ] Go HTTP server for web UI
- [ ] Terminal proxy (HTTP and WebSocket)
- [ ] Host metrics endpoint
- [ ] Update index.html for k8s API integration

### Phase 6: Helm Chart Migration
- [ ] Remove Python API deployment templates
- [ ] Add operator deployment manifest
- [ ] Add dashboard deployment manifest
- [ ] Update RBAC with CRD permissions
- [ ] Update values.yaml structure
- [ ] Create kustomize overlays

### Phase 7: Testing & Validation
- [ ] Local testing with `make run`
- [ ] E2E tests for cluster lifecycle
- [ ] Finalizer cleanup testing
- [ ] Multi-ingress type testing
- [ ] Build Docker images

### Phase 8: Cleanup
- [ ] Remove old Python code (main.py, requirements.txt)
- [ ] Update documentation
- [ ] Create migration guide

## Git Commits

1. `fcf4fdd` - WIP: Initialize Kubebuilder operator
2. `2265dd8` - feat: Implement comprehensive KloneCluster CRD
3. (next) - feat: Implement controller reconciliation logic

## Notes

- Using MD5 hash for deterministic CIDR allocation (10.100-149.x.x for cluster, 10.150-199.x.x for services)
- Finalizer: `klone.io/cleanup` for proper resource cleanup
- Tailscale operator finalizer conflict requires double-clearing strategy
- Terminal pod uses caching for kubectl/ttyd/bash binaries on PVC

## Next Steps

1. Implement resource builder functions (namespace, PV, PVC, Services)
2. Implement StatefulSet builder for k3s control plane
3. Implement Deployment builders for workers and terminal
4. Implement ingress builders for each type
5. Wire up reconciliation loop
6. Add finalizer handling
7. Implement status updates
