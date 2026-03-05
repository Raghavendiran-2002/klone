# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Kubernetes Operator project that manages **KloneCluster** resources - a system for creating nested Kubernetes clusters inside an existing cluster using k3s. Each KloneCluster creates an isolated k3s cluster with its own control plane, workers, and optional web terminal access.

**Key capabilities:**
- Deploy nested k3s clusters with configurable control plane and worker nodes
- Automatic CIDR allocation to prevent network conflicts between multiple clusters
- Web terminal access via ttyd with automatic kubeconfig provisioning
- Multiple ingress options: Tailscale, AWS ALB, or none
- Optional metrics-server auto-installation in nested clusters
- Web dashboard for visualizing and managing all KloneCluster resources

## Project Structure

This is a **Kubebuilder v4** project using the standard single-group layout:

```
klone/                         (repository root)
├── .github/                   GitHub workflows and issue templates
│   ├── workflows/             CI/CD workflows
│   └── ISSUE_TEMPLATE/        Bug reports and feature requests
├── api/v1alpha1/              CRD definitions (KloneCluster)
├── cmd/                       Entry points
├── config/                    Kubernetes manifests and kustomize configs
│   ├── crd/bases/             Generated CRDs (DO NOT EDIT)
│   ├── rbac/                  Generated RBAC + static roles
│   ├── samples/               Example KloneCluster CRs
│   ├── dashboard/             Dashboard deployment manifests
│   └── default/               Default kustomize overlay
├── dashboard/                 Web dashboard Go application
├── docs/                      Additional documentation
├── examples/                  Sample KloneCluster manifests
├── internal/controller/       Reconciliation logic (split across multiple files)
├── test/                      Unit and E2E tests
├── CLAUDE.md                  This file
├── CONTRIBUTING.md            Contribution guidelines
├── LICENSE                    Apache 2.0 License
├── Makefile                   Build, test, and deployment targets
└── README.md                  Main documentation
```

## Build and Development Commands

All commands should be run from the repository root directory.

**Standard Build and Deploy Process**: When building and deploying new versions, always use:
```bash
IMG=raghavendiran2002/klone-operator:v1.0.49 make docker-build deploy
```
This command builds a multi-architecture Docker image (amd64 + arm64) and deploys it to the cluster.

### Local Development
```bash
make manifests generate fmt vet   # Regenerate CRDs/RBAC and format code
make build                        # Build manager binary to bin/manager
make run                          # Run controller locally (uses ~/.kube/config)
make test                         # Run unit tests with envtest
make lint                         # Run golangci-lint
make lint-fix                     # Auto-fix linting issues
```

### Docker Image Management
```bash
# Build and push operator image (set IMG to your registry)
IMG=raghavendiran2002/klone-operator:v1.0.24 make docker-build
IMG=raghavendiran2002/klone-operator:v1.0.24 make docker-push

# Or combined:
IMG=raghavendiran2002/klone-operator:v1.0.24 make docker-build docker-push
```

### Cluster Deployment
```bash
# Install CRDs only
make install

# Deploy operator to cluster
IMG=raghavendiran2002/klone-operator:v1.0.24 make deploy

# Create a sample cluster
kubectl apply -f config/samples/klone_v1alpha1_klonecluster.yaml

# Remove operator
make undeploy

# Remove CRDs
make uninstall
```

### Testing
```bash
# Unit tests (uses envtest)
make test

# E2E tests (creates a Kind cluster automatically)
make test-e2e

# Cleanup test cluster
make cleanup-test-e2e
```

## Architecture

### Controller Design

The `KloneClusterReconciler` is split across multiple files for maintainability:

- **klonecluster_controller.go** (661 lines): Main reconciliation loop, finalizer handling, status management, CIDR allocation
- **workloads.go**: Creates StatefulSets (control plane), Deployments (workers, terminal), Services
- **resources.go**: Creates Namespaces, PersistentVolumes, PersistentVolumeClaims, ConfigMaps
- **ingress.go**: Handles Tailscale/ALB ingress creation based on spec.ingress.type
- **cleanup.go**: Finalizer logic for graceful deletion and orphan cleanup
- **restart.go**: Handles ConfigMap changes and triggers workload restarts
- **utils.go**: CIDR allocation, helper functions

### Reconciliation Flow

1. **Namespace Creation**: Creates a dedicated namespace named after the cluster
2. **Storage Setup**: Creates PV (hostPath) and PVC for k3s data persistence
3. **CIDR Allocation**: Assigns unique cluster-cidr and service-cidr to prevent conflicts
4. **Control Plane**: StatefulSet running k3s server with allocated CIDRs
5. **Worker Nodes**: Deployment running k3s agents connecting to control plane
6. **Terminal**: Deployment with ttyd providing web-based kubectl access
7. **Ingress**: Creates Ingress resource (Tailscale or ALB) based on configuration
8. **Metrics Server**: Job that installs metrics-server into the nested cluster (if enabled)
9. **Status Updates**: Continuously updates .status with workload states, URLs, and conditions

### CIDR Management

**Critical for multi-cluster scenarios**: Each KloneCluster gets unique CIDRs to avoid IP conflicts when nested clusters route through the parent cluster.

- Allocation is deterministic based on cluster name hash
- `AllocateCIDRs()` in utils.go generates: `10.{hash1}.0.0/16` (pods) and `10.{hash2}.0.0/16` (services)
- Stored in `status.clusterCIDR` and `status.serviceCIDR`
- Passed to k3s via `--cluster-cidr` and `--service-cidr` flags

### Terminal Access

The terminal pod provides web-based access to the nested cluster:

1. **Kubeconfig Setup**: An init container extracts the kubeconfig from the k3s control plane
2. **Server Replacement**: The kubeconfig's server URL is rewritten to point to the control plane Service
3. **ttyd Server**: Runs `ttyd sh` to provide a web shell with `kubectl` available
4. **Authentication**: The terminal pod has the kubeconfig mounted and kubectl configured

### Dashboard

A separate Go application (`dashboard/main.go`) that provides a web UI:

- Lists all KloneCluster resources across the parent cluster
- Shows status, workload health, and ingress URLs
- Proxies terminal access through `/api/terminal/{namespace}/`
- Deployed via `config/dashboard/deployment.yaml`

## Making Changes

### Modifying the CRD

1. Edit `api/v1alpha1/klonecluster_types.go`
2. Add/modify kubebuilder markers (e.g., `+kubebuilder:validation:Enum=...`)
3. Run `make manifests generate` to regenerate CRDs and DeepCopy methods
4. Run `make install` to update CRDs in your cluster
5. Run `make test` to ensure tests pass

### Adding Controller Logic

The controller logic is split across multiple files. Choose the appropriate file:

- **New workload types**: Add to `workloads.go`
- **New resource types (ConfigMap, Secret)**: Add to `resources.go`
- **Ingress changes**: Edit `ingress.go`
- **Cleanup/deletion logic**: Edit `cleanup.go`
- **Main reconciliation flow**: Edit `klonecluster_controller.go`

Always maintain the existing pattern:
1. Check if resource exists
2. Create with owner reference if missing
3. Update status to reflect actual state

### Updating RBAC

The operator needs permissions to create resources in the parent cluster:

- Add RBAC markers in controller files: `// +kubebuilder:rbac:groups=...,resources=...,verbs=...`
- Run `make manifests` to regenerate `config/rbac/role.yaml`
- Do NOT manually edit `config/rbac/role.yaml` - it's auto-generated

### Testing Changes Locally

```bash
# 1. Regenerate manifests and code
make manifests generate

# 2. Run tests
make test

# 3. Run controller locally (no Docker needed)
make run

# 4. In another terminal, create a test cluster
kubectl apply -f test-klonecluster.yaml

# 5. Watch logs and status
kubectl logs -f -l control-plane=controller-manager -n operator-system
kubectl get klonecluster test-cluster -o yaml
```

### Building and Deploying Updates

```bash
# 1. Increment version tag
export VERSION=v1.0.25

# 2. Build and push
IMG=raghavendiran2002/klone-operator:$VERSION make docker-build docker-push

# 2.1. Build and push and deploy  ( use mostly this one)
IMG=raghavendiran2002/klone-operator:$VERSION make docker-build docker-push deploy

# 3. Deploy to cluster
IMG=raghavendiran2002/klone-operator:$VERSION make deploy

# 4. Verify deployment
kubectl get pods -n operator-system
kubectl logs -f -n operator-system -l control-plane=controller-manager
```

## Common Issues and Solutions

### Nested kubectl Not Working

Check that:
1. Control plane Service is running and has an endpoint
2. Kubeconfig in terminal pod has correct server URL (`https://klone-controlplane:6443`)
3. Terminal pod has network access to control plane Service
4. k3s token matches between control plane and terminal ConfigMap

### CIDR Conflicts

If nested clusters can't communicate or have routing issues:
1. Check `status.clusterCIDR` and `status.serviceCIDR` are set
2. Verify k3s server is running with correct `--cluster-cidr` and `--service-cidr`
3. Ensure no overlap with parent cluster CIDRs (usually `10.42.0.0/16` for k3s)

### Ingress Not Created

1. Check `spec.ingress.type` is set to `tailscale` or `loadbalancer` (not `none`)
2. For Tailscale: Verify tailscale operator is installed in the parent cluster
3. For ALB: Verify AWS Load Balancer Controller is installed and has proper IAM permissions

### Orphaned Resources

If resources remain after deleting a KloneCluster:
1. Check if finalizer is properly removing child resources
2. Manually delete the namespace: `kubectl delete namespace <cluster-name>`
3. Delete PV: `kubectl delete pv klone-<cluster-name>-pv`
4. Check for stuck Ingress resources

### Metrics Server Fails to Install

1. Check the metrics-server Job logs: `kubectl logs -n <cluster-name> job/install-metrics-server`
2. Verify the terminal pod can exec into kubectl
3. Check if nested cluster has internet access (metrics-server image pull)

## Important Notes

- **DO NOT** manually edit files in `config/crd/bases/` or `config/rbac/role.yaml` - they are auto-generated
- **DO NOT** remove `// +kubebuilder:scaffold:*` comments - Kubebuilder uses these as injection points
- **ALWAYS** run `make manifests generate` after modifying `*_types.go` files
- Each KloneCluster gets its own namespace, PV, and isolated network (via CIDRs)
- The controller uses finalizers to ensure cleanup - do not remove the finalizer logic
- Terminal pods need proper kubeconfig with the Service DNS name, not localhost
