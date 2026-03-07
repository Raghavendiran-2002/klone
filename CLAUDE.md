# CLAUDE.md

This file provides guidance to Claude Code when working with the Klone Operator repository.

## Project Overview

**Klone Operator** is a Kubernetes Operator that creates nested Kubernetes clusters inside an existing cluster using k3s. It's designed for K8s training environments (similar to KodeKloud labs), enabling dynamic provisioning of isolated Kubernetes clusters for students or testing.

**Core Functionality:**
- Deploy complete k3s clusters (control plane + workers) as pods within a parent cluster
- Each nested cluster gets its own namespace, persistent storage, and unique network CIDRs
- Web-based terminal access (ttyd) with pre-configured kubectl for instant access
- Multiple ingress options: Tailscale, AWS ALB, or none
- Optional metrics-server and ArgoCD integration
- Declarative management via KloneCluster CRD

**Architecture:**
- Built with Kubebuilder v4
- Controller split across multiple files for maintainability
- Automatic CIDR allocation prevents IP conflicts between nested clusters
- Finalizers ensure graceful cleanup of all child resources
- Web dashboard for visualizing cluster health and status

**Use Case:** Training environments where each student/team needs an isolated Kubernetes cluster with full admin access, without requiring separate physical/cloud infrastructure per cluster.

## Version Management

**Current Versioning:**
- Operator: `v1.0.52` (semantic versioning: MAJOR.MINOR.PATCH)
- Dashboard: `v1.0.12`
- Helm Chart: `1.0.52` (matches operator, strips 'v' prefix)
- Docker Registry: `raghavendiran2002/klone-operator` and `raghavendiran2002/klone-dashboard`

**Critical: Version files must stay synchronized**

When updating versions, these 4 files MUST be updated together:
1. `helm/klone-operator/Chart.yaml` - Lines 5-6 (version + appVersion)
2. `helm/klone-operator/values.yaml` - Line 13 (image.tag)
3. `config/manager/kustomization.yaml` - Line 8 (newTag)
4. `config/dashboard/deployment.yaml` - Line 21 (image tag) - Only for dashboard releases

**Helm Chart Version Strategy:**
- Chart version = operator version (both increment together)
- Chart uses `1.0.52` (no 'v'), appVersion uses `"v1.0.52"` (with 'v')

## Build and Deploy Workflow

**Standard Development Flow:**
```bash
# If api/ directory changed, regenerate CRDs and code
make manifests generate fmt

# Build multi-arch image (amd64 + arm64) and deploy
IMG=raghavendiran2002/klone-operator:v1.0.52 make docker-build deploy

# Dashboard build (separate)
docker buildx build --platform linux/amd64,linux/arm64 \
  -t raghavendiran2002/klone-dashboard:v1.0.12 \
  -f dashboard/Dockerfile --push .
```

**IMPORTANT: Multi-Architecture Builds**
- Always build for both amd64 and arm64 using buildx
- This is required for M-series Mac support and AWS Graviton instances
- The `make docker-build` target handles this automatically

**Key Make Targets:**
- `make manifests` - Regenerate CRDs from api/ types (run after changing *_types.go)
- `make generate` - Regenerate DeepCopy methods (run after changing *_types.go)
- `make fmt` - Format Go code
- `make test` - Run unit tests with envtest
- `make build` - Build manager binary to bin/manager
- `make install` - Install CRDs to cluster
- `make deploy IMG=...` - Deploy operator to cluster
- `make docker-build IMG=...` - Build and push multi-arch Docker image

**Pre-build Detection:**
- If `api/v1alpha1/` files changed: MUST run `make manifests generate` before building
- This regenerates CRD YAML and Go code
- Skipping this causes deployment failures due to CRD/code mismatch

## Project Structure

```
klone/
├── api/v1alpha1/              # CRD definitions (KloneCluster spec/status)
│   └── klonecluster_types.go  # Edit this to add fields, then run `make manifests generate`
├── cmd/main.go                # Operator entry point
├── internal/controller/       # Reconciliation logic (split across files)
│   ├── klonecluster_controller.go  # Main reconcile loop, finalizers, CIDR allocation
│   ├── workloads.go           # StatefulSet (control plane), Deployments (workers, terminal)
│   ├── resources.go           # Namespace, PV, PVC, ConfigMap creation
│   ├── ingress.go             # Tailscale and ALB ingress handling
│   ├── cleanup.go             # Finalizer deletion logic
│   ├── restart.go             # ConfigMap change detection
│   └── utils.go               # CIDR allocation, helper functions
├── dashboard/                 # Web UI for visualizing KloneClusters
├── config/
│   ├── crd/bases/             # Generated CRDs (DO NOT EDIT MANUALLY)
│   ├── rbac/role.yaml         # Generated RBAC (DO NOT EDIT MANUALLY)
│   ├── manager/               # Operator deployment
│   ├── dashboard/             # Dashboard deployment
│   └── samples/               # Example KloneCluster CRs
├── helm/klone-operator/       # Helm chart for production deployment
├── examples/                  # Sample manifests
└── test/                      # Unit and E2E tests
```

## Controller Architecture

**Reconciliation Flow:**
1. Namespace creation (named after cluster)
2. Storage setup (hostPath PV + PVC for k3s data)
3. CIDR allocation (unique cluster-cidr and service-cidr)
4. Control plane (StatefulSet running k3s server)
5. Worker nodes (Deployment running k3s agents)
6. Terminal (Deployment with ttyd + kubectl configured)
7. Ingress (Tailscale or ALB based on spec.ingress.type)
8. Metrics server installation (Job that runs in nested cluster)
9. Status updates (track workload states, URLs, readiness)

**CIDR Management (Critical for Multi-Cluster):**
- Each KloneCluster MUST have unique CIDRs to avoid routing conflicts
- Allocation is deterministic based on hash of cluster name
- `AllocateCIDRs()` in utils.go generates: `10.{hash1}.0.0/16` (pods) and `10.{hash2}.0.0/16` (services)
- CIDRs stored in `status.clusterCIDR` and `status.serviceCIDR`
- Passed to k3s via `--cluster-cidr` and `--service-cidr` flags

**Terminal Access:**
- Init container extracts kubeconfig from control plane
- **CRITICAL**: Server URL must be `https://klone-controlplane:6443` (Service DNS), NOT localhost
- ttyd provides web shell at port 7681
- kubectl is pre-installed and configured

**Controller File Organization:**
- Add new workload types → `workloads.go`
- Add new resource types → `resources.go`
- Modify ingress logic → `ingress.go`
- Change deletion behavior → `cleanup.go`
- Modify main reconcile flow → `klonecluster_controller.go`

## Development Patterns

**Adding/Modifying CRD Fields:**
1. Edit `api/v1alpha1/klonecluster_types.go`
2. Add kubebuilder markers for validation (e.g., `+kubebuilder:validation:Enum=...`)
3. Run `make manifests generate` to regenerate CRDs and DeepCopy
4. Run `make install` to update CRDs in cluster
5. Update controller logic in appropriate file
6. Run `make test`

**Adding Controller Permissions:**
- Add RBAC markers: `// +kubebuilder:rbac:groups=...,resources=...,verbs=...`
- Run `make manifests` to regenerate `config/rbac/role.yaml`
- Never manually edit `config/rbac/role.yaml` (auto-generated)

**Testing Changes Locally:**
```bash
# Run controller locally (no Docker needed, uses current kubeconfig)
make run

# In another terminal
kubectl apply -f test-cluster.yaml
kubectl get klonecluster -w
```

## Git Workflow

**Required Configuration:**
- Git user email: `raghavendiran46461@gmail.com`
- Never add `Co-Authored-By: Claude <noreply@anthropic.com>`
- Use conventional commits format

**Conventional Commit Format:**
```
<type>(<scope>): <subject>

<optional body>
```

**Types:**
- `feat`: New feature (e.g., ArgoCD integration, new CRD field)
- `fix`: Bug fix (e.g., CIDR allocation conflict)
- `perf`: Performance improvement (e.g., Docker build optimization)
- `refactor`: Code restructuring without behavior change
- `docs`: Documentation updates (README, comments)
- `chore`: Build/config changes (Makefile, CI/CD, version bumps)

**Examples:**
- `feat(argocd): Add automatic cluster registration to host ArgoCD`
- `fix(terminal): Correct kubeconfig server URL to use Service DNS`
- `perf(build): Optimize Docker multi-stage builds for faster CI`

## Important Reminders

**DO NOT:**
- Manually edit `config/crd/bases/` (regenerated by `make manifests`)
- Manually edit `config/rbac/role.yaml` (regenerated by `make manifests`)
- Remove `// +kubebuilder:scaffold:*` comments (Kubebuilder injection points)
- Remove finalizers from controller (critical for resource cleanup)
- Use `localhost` in terminal kubeconfig (use Service DNS: `klone-controlplane`)

**ALWAYS:**
- Run `make manifests generate` after modifying `*_types.go` files
- Build multi-arch images (amd64 + arm64)
- Keep version files synchronized (4 files listed above)
- Test with `make test` before committing significant changes
- Use owner references for child resources (automatic garbage collection)

**Resource Naming Patterns:**
- Namespace: `{cluster-name}` (e.g., `test-cluster`)
- PV: `klone-{cluster-name}-pv`
- PVC: `klone-data` (in cluster namespace)
- Control plane Service: `klone-controlplane`
- Terminal Deployment: `klone-terminal`

## Error Handling Patterns

**Retry Logic:**
- Failed operations (build, deploy) should retry once
- After second failure, stop and report detailed error
- Never silently ignore errors

**Common Failure Scenarios:**
1. **kubectl not working in terminal**: Check Service exists and kubeconfig has correct server URL
2. **CIDR conflicts**: Verify unique CIDRs in status, check k3s server logs for CIDR flags
3. **Ingress not created**: Verify type is not 'none', check if Tailscale/ALB controller is installed
4. **Orphaned resources after deletion**: Check finalizer execution logs

## Helm Chart Management

**Chart Location:** `helm/klone-operator/`

**Key Files:**
- `Chart.yaml` - Chart metadata (version, appVersion)
- `values.yaml` - Default values (image tags, resources, replicas)
- `templates/` - Kubernetes manifests with templating

**Publishing Flow (automated by CI/CD):**
1. Chart packaged: `helm package helm/klone-operator`
2. Uploaded to `gh-pages` branch
3. Repository index updated: `helm repo index`
4. Available at: `https://raghavendiran-2002.github.io/klone/helm-charts`

## Testing

**Unit Tests:**
```bash
make test  # Uses envtest (fake Kubernetes API)
```

**E2E Tests:**
```bash
make test-e2e         # Creates Kind cluster, deploys operator, tests KloneCluster creation
make cleanup-test-e2e # Cleanup
```

**Manual Testing:**
```bash
# Create test cluster
kubectl apply -f examples/test-cluster.yaml

# Monitor
kubectl get klonecluster test-cluster -w
kubectl get pods -n test-cluster

# Access terminal
kubectl port-forward -n test-cluster svc/klone-terminal 7681:7681
# Open http://localhost:7681

# Verify nested cluster
kubectl exec -n test-cluster deployment/klone-terminal -- kubectl get nodes

# Cleanup
kubectl delete klonecluster test-cluster
```

## Additional Resources

- Full documentation: `README.md`
- Example manifests: `examples/`
- GitHub Actions workflows: `.github/workflows/`
- Kubebuilder Book: https://book.kubebuilder.io/
