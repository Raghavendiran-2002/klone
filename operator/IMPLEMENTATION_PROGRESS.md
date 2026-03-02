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
- [x] Resource builders created:
  - [x] Namespace creation
  - [x] PersistentVolume creation (with optional node affinity)
  - [x] PersistentVolumeClaim creation
  - [x] Services creation (headless control-plane, ClusterIP terminal)
  - [x] K3s control plane StatefulSet
  - [x] K3s worker Deployment
  - [x] Terminal Deployment with caching logic (kubectl, ttyd, bash)
  - [x] Ingress handlers:
    - [x] Tailscale ingress
    - [x] AWS ALB ingress
    - [x] None (no ingress)
  - [x] CIDR allocation utility (MD5-based)
- [ ] Main reconciliation loop
- [ ] Finalizer and cleanup logic
- [ ] Status update logic
- [ ] Restart annotation support

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
