# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Klone** is a Kubernetes-in-Kubernetes training platform that runs nested k3s clusters as pods inside a parent Kubernetes cluster. It provides on-demand, isolated Kubernetes environments with web-based terminal access.

**Core Technology Stack:**
- Python 3.12 + FastAPI (cluster manager API)
- k3s (lightweight Kubernetes for nested clusters)
- Helm (deployment/templating)
- ttyd (web terminal)
- Tailscale (ingress networking)

## Architecture

### Two-Component Design

1. **Dynamic Cluster Manager (klone-api)** - FastAPI service that creates/manages multiple nested k3s clusters on-demand
2. **Static Cluster (optional)** - Pre-deployed k3s cluster always available

### How Nested k3s Works

Each dynamically created cluster gets:
- Dedicated namespace with label `klone-managed=true`
- StatefulSet (k3s control plane with SQLite datastore)
- Deployment (k3s worker nodes)
- Deployment (Alpine terminal pod with kubectl + ttyd)
- PersistentVolume on host path `/home/raghav/klone/{cluster-name}`
- Unique network CIDRs derived from MD5 hash of cluster name
- Tailscale Ingress for terminal access

**Critical Design Choices:**
- **Flannel backend: host-gw** (not VXLAN) - Multiple clusters on same node would conflict on `flannel.1` interface
- **SQLite datastore** (not embedded etcd) - Uses ~10MB RAM vs 150-300MB, allows more clusters per node
- **Privileged containers** - Required for nested Kubernetes functionality
- **Double finalizer clearing** - Tailscale operator re-adds finalizers; need to strip before/after delete + background cleanup

## Key Files

| File | Purpose |
|------|---------|
| `helm-chart/files/main.py` | FastAPI cluster manager - handles cluster lifecycle, metrics, terminal proxy |
| `helm-chart/files/index.html` | Web UI dashboard with real-time cluster status and inline terminal |
| `helm-chart/values.yaml` | Configuration for API manager, static cluster, networking |
| `helm-chart/templates/klone/*.yaml` | Dynamic cluster manager deployment (API server) |
| `helm-chart/templates/klone-static/*.yaml` | Optional pre-deployed k3s cluster |
| `metrics-dashboard.sh` | Bash script for monitoring host cluster resource usage |

## Common Commands

### Deploy/Update Helm Chart

```bash
# Validate chart
helm lint helm-chart

# Preview rendered templates
helm template klone-training helm-chart --namespace klone

# Install fresh
helm install klone-training helm-chart \
  --namespace klone \
  --create-namespace \
  --set kloneApi.tailscaleDomain=your-domain.ts.net

# Upgrade after code changes
helm upgrade klone-training helm-chart --namespace klone

# Quick update for main.py changes (faster than helm upgrade)
kubectl delete configmap klone -n klone && \
kubectl create configmap klone -n klone \
  --from-file=main.py=helm-chart/files/main.py \
  --from-file=index.html=helm-chart/files/index.html \
  --from-file=requirements.txt=helm-chart/files/requirements.txt && \
kubectl rollout restart deployment/klone -n klone
```

### Verify Deployment

```bash
# Check all pods
kubectl get pods -n klone

# Check API logs
kubectl logs -n klone -l app=klone -c server --tail=100 -f

# Test health endpoint
kubectl exec -n klone deployment/klone -c server -- \
  curl -s http://localhost:8000/healthz

# Test metrics endpoint
kubectl exec -n klone deployment/klone -c server -- \
  curl -s http://localhost:8000/api/metrics/host
```

### Debug Nested Clusters

```bash
# List managed clusters
kubectl get ns -l klone-managed=true

# Check control plane logs
kubectl logs -n {cluster-name} k3s-control-plane-0

# Check worker logs
kubectl logs -n {cluster-name} -l app=k3s-worker

# Check terminal pod logs
kubectl logs -n {cluster-name} -l app=klone-terminal

# Access nested cluster directly
kubectl exec -n {cluster-name} -it k3s-control-plane-0 -- \
  k3s kubectl get nodes

# Extract kubeconfig
kubectl exec -n {cluster-name} k3s-control-plane-0 -- \
  cat /var/lib/rancher/k3s/kubeconfig.yaml > temp-kubeconfig.yaml
```

### Manual Cluster Cleanup

```bash
# Force delete stuck namespace (removes finalizers)
kubectl patch namespace {cluster-name} -p '{"metadata":{"finalizers":null}}'
kubectl delete namespace {cluster-name} --grace-period=0 --force

# Clean up PV
kubectl delete pv klone-pv-{cluster-name}

# Clean up host path data
ssh node-with-primary-label
rm -rf /home/raghav/klone/{cluster-name}
```

## Code Patterns

### main.py Function Responsibilities

**Synchronous Cluster Operations:**
- `_do_create_cluster(name)` - Creates namespace, PV, PVC, Services, StatefulSets, Deployments, Ingress
- `delete_cluster(name)` - Strips finalizers, deletes namespace and PV
- `restart_cluster(name)` - Patches deployment with restart annotation

**Asynchronous Background Tasks:**
- `_install_metrics_server(name)` - Waits for terminal pod ready, then kubectl applies metrics-server manifest
- `_cleanup_terminating_namespaces()` - Runs every 30s to re-strip finalizers from stuck namespaces

**Metrics Collection:**
- `get_host_metrics()` - Queries host cluster's metrics.k8s.io API
- `get_cluster_metrics(name)` - Execs into terminal pod, runs `kubectl top nodes/pods`, parses output

**Terminal Proxy:**
- `proxy_http(name, path)` - HTTP proxy to `klone-terminal.{namespace}.svc:80`
- `proxy_ws(name)` - WebSocket proxy for terminal bidirectional communication

### Network CIDR Allocation

```python
# Unique CIDRs per cluster to avoid routing conflicts
import hashlib
hash_int = int(hashlib.md5(name.encode()).hexdigest()[:8], 16)
cluster_cidr = f"10.{100 + (hash_int % 50)}.0.0/16"
service_cidr = f"10.{150 + (hash_int % 50)}.0.0/16"
```

### Metrics Parsing Units

CPU formats: `1782u` (microcores), `150m` (millicores), `2n` (nanocores), `2` (cores)
Memory formats: `1024Mi`, `2Gi`, `512000Ki`

```python
# Always convert to millicores
if cpu_str.endswith("u"):
    cpu_millicores = int(cpu_str[:-1]) / 1000
elif cpu_str.endswith("m"):
    cpu_millicores = int(cpu_str[:-1])
```

## Configuration Values

### Essential values.yaml Settings

```yaml
kloneApi:
  tailscaleDomain: "taile3ca5.ts.net"  # MUST match your Tailscale network
  nodeAffinity:
    enabled: true
    label: "primary"  # Node must have workload=primary label for PV binding

klone:
  k3s:
    image: "rancher/k3s:v1.35.1-k3s1"  # Can upgrade k3s version
    token: "supersecrettoken123"  # Shared secret for agent auth
  storage:
    storageClass: "local-path"  # Must exist in cluster
    hostPath: "/home/raghav/klone"  # Base path for all cluster PVs
```

### When to Bump Chart Version

Edit `helm-chart/Chart.yaml`:
- **Patch version** (0.4.5 → 0.4.6): Bug fixes, metric parsing changes
- **Minor version** (0.4.6 → 0.5.0): New features like metrics-server auto-install
- **Major version** (0.5.0 → 1.0.0): Breaking changes to API or cluster architecture

## API Endpoints Reference

```
GET  /                                  - Web UI dashboard
GET  /healthz                           - Health check (200 OK)
GET  /api/clusters                      - List all clusters with status
POST /api/clusters                      - Create cluster (body: {"name": "dev"})
DELETE /api/clusters/{name}             - Delete cluster and PV
POST /api/clusters/{name}/restart       - Restart cluster pods
GET  /api/clusters/{name}/status        - Get cluster status
GET  /api/clusters/{name}/metrics       - Get nested cluster metrics
GET  /api/clusters/{name}/terminal-probe - Check if terminal pod is ready (for UI polling)
GET  /api/metrics/host                  - Get host cluster node metrics
GET  /proxy/{name}/{path}               - HTTP proxy to terminal
WS   /proxy/{name}/ws                   - WebSocket proxy to terminal
```

## RBAC Permissions Required

The `klone` ServiceAccount needs extensive cluster-level permissions:

**Why so broad?** The API dynamically creates full k8s resources (namespaces, PVs, StatefulSets, Ingresses) across the entire cluster.

**Critical permissions:**
- Namespaces: CRUD + finalize (for cleanup)
- PersistentVolumes: Create, delete
- Pods/Exec: Execute commands in terminal pods for metrics
- Secrets: Patch finalizers (Tailscale operator interaction)
- Metrics API: Read node/pod metrics

See `helm-chart/templates/klone/clusterrole.yaml` for full list.

## Common Issues & Solutions

### Namespace Stuck in Terminating

**Cause:** Tailscale operator adds finalizers to Ingresses/Secrets
**Fix:** Run background cleanup task or manually patch:
```bash
kubectl patch ingress klone-terminal -n {namespace} -p '{"metadata":{"finalizers":null}}'
kubectl patch secret tailscale-auth -n {namespace} -p '{"metadata":{"finalizers":null}}'
```

### Terminal Shows "Terminal is starting up..." Forever

**Cause:** ttyd binary download failed or pod not ready
**Debug:**
```bash
kubectl logs -n {cluster-name} -l app=klone-terminal
# Look for: "Caching ttyd (first run)..."
# Check for: wget errors, permission issues
```

### Cluster Control Plane CrashLoopBackOff

**Cause:** Old etcd data causing "cluster ID mismatch"
**Fix:** Init container should clear `/var/lib/rancher/k3s/server/db/etcd` - verify it's running

### Host Metrics Return 503

**Cause:** metrics-server not installed in host cluster OR CPU parsing error
**Debug:**
```bash
kubectl top nodes  # Should work in host cluster
kubectl logs -n klone -l app=klone | grep -i "error.*metrics"
```

### Workers Not Joining Control Plane

**Cause:** Headless service DNS not resolving or wrong namespace
**Debug:**
```bash
kubectl exec -n {cluster-name} -l app=k3s-worker -- \
  nslookup k3s-control-plane.{cluster-name}.svc.cluster.local
```

## Development Workflow

### Changing main.py Logic

1. Edit `helm-chart/files/main.py`
2. Quick deploy: Delete ConfigMap + recreate + rollout restart (see commands above)
3. Test via kubectl exec or Tailscale ingress
4. Bump `Chart.yaml` version
5. Run `helm upgrade`

### Changing Cluster Creation Logic

**Template Changes:**
1. Edit deployment template in `_do_create_cluster()` function
2. Test by creating new cluster via API
3. Delete test cluster
4. Verify cleanup works correctly

**Critical:** Always test finalizer cleanup - stuck namespaces are the #1 operational issue

### Changing Web UI (index.html)

1. Edit `helm-chart/files/index.html`
2. Update ConfigMap (same quick deploy as main.py)
3. Hard refresh browser (Cmd+Shift+R) to bypass cache
4. Check browser console for JavaScript errors

### Testing Metrics Changes

```bash
# Test host metrics parsing
kubectl exec -n klone deployment/klone -c server -- python3 -c "
import requests
resp = requests.get('http://localhost:8000/api/metrics/host')
print(resp.status_code, resp.json())
"

# Test nested cluster metrics
kubectl exec -n klone deployment/klone -c server -- python3 -c "
import requests
resp = requests.get('http://localhost:8000/api/clusters/{cluster-name}/metrics')
print(resp.status_code, resp.json())
"
```

## Web UI Implementation Details

**Framework:** Vanilla JavaScript with Tailwind CSS (no build step)
**State Management:** Polling-based (no WebSocket except for terminal)
**Refresh Intervals:**
- Cluster list: 8 seconds
- Host metrics (header): 15 seconds
- Terminal probe: 3 seconds (when waiting for ready)

**Key Functions:**
- `addCluster()` - Validates name format, POSTs to API
- `connectTerminal(name)` - Polls terminal-probe until ready, opens modal with iframe
- `toggleMetrics(name)` - Fetches nested cluster metrics, displays in expandable section
- `deleteCluster(name)` - Confirmation dialog, DELETE request

## Storage Management

### HostPath PV Strategy

Each cluster gets dedicated PV:
- Path: `/home/raghav/klone/{cluster-name}`
- Node affinity: `workload=primary` label
- Reclaim policy: `Retain` (manual cleanup)
- Subdirectories:
  - `/k3s/server/` - SQLite database
  - `/k3s/term-cache/` - Cached binaries (kubectl, ttyd, bash.apk)

**Why HostPath?** Training platform assumption - single-node or manual data management. Production would use NFS/Ceph.

### Cache Optimization in Terminal Pod

Init container logic:
```bash
CACHE=/k3s/term-cache  # On PVC
# Download kubectl once, reuse on restarts
if [ ! -x "$CACHE/kubectl" ]; then
  curl -Lo $CACHE/kubectl https://...
fi
ln -sf $CACHE/kubectl /usr/local/bin/kubectl
```

Speeds up terminal pod restarts from ~30s to ~5s.

## Metrics-Server Auto-Installation

When a cluster is created, `_install_metrics_server()` runs asynchronously:

1. Wait up to 5 minutes for terminal pod to be Running + Ready
2. Exec into terminal pod
3. Apply metrics-server YAML manifest via `kubectl apply -f -`
4. Configure with `--kubelet-insecure-tls` flag (nested kubelet uses self-signed certs)

**Known Limitation:** Metrics-server in nested k3s cannot reach kubelet due to container networking. The code implements graceful fallback showing node count instead of metrics.

## Tailscale Integration

**Ingress Pattern:**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: klone
spec:
  ingressClassName: tailscale
  tls:
    - hosts:
        - klone  # Becomes klone.{tailscaleDomain}
  rules:
    - http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: klone
                port:
                  name: http
```

**Finalizer Behavior:** Tailscale operator adds finalizers to Ingress and companion Secret. Must be stripped before namespace deletion succeeds.

## Platform Requirements

**Host Cluster:**
- Kubernetes 1.28+ (tested on k3s, should work on any distro)
- Tailscale operator installed and authenticated
- metrics-server installed (for host metrics)
- StorageClass `local-path` or `local-storage`
- At least one node with label `workload=primary`

**Node Resources:**
- Minimum per nested cluster: ~600MB RAM (control plane + 2 workers)
- Recommended: 8GB+ RAM per physical node to run 5-10 nested clusters

**Privileges:**
- k3s containers must run privileged (security context allows)
- Not suitable for untrusted multi-tenant environments

## Version History Context

**Current:** 0.4.6 (Chart), 2.0.5 (App)

**Recent Changes:**
- 0.4.6: Added microcore (u suffix) support in CPU parsing
- 0.4.5: Implemented metrics-server auto-installation
- 0.4.4: Switched from gotty to ttyd for terminals
- 0.4.2: Updated Tailscale ingress to recommended pattern
- Earlier: Moved from VXLAN to host-gw, implemented finalizer cleanup, added background tasks
