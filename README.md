# Klone Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-v1.11.3+-blue.svg)](https://kubernetes.io/)
[![Go Version](https://img.shields.io/badge/Go-1.25.3+-00ADD8.svg)](https://go.dev/)

A Kubernetes Operator for creating and managing nested Kubernetes clusters using k3s. Deploy isolated, lightweight Kubernetes clusters inside your existing cluster with full control plane, worker nodes, web-based terminal access, and flexible ingress options.

## 🌟 Features

- **🎯 Nested Kubernetes Clusters**: Create fully functional k3s clusters within your parent Kubernetes cluster
- **🔒 Network Isolation**: Automatic CIDR allocation prevents conflicts between multiple nested clusters
- **🖥️ Web Terminal Access**: Built-in web terminal with kubectl pre-configured for instant access
- **🌐 Flexible Ingress**: Support for Tailscale, AWS Application Load Balancer, or no ingress
- **📊 Metrics Support**: Optional auto-installation of metrics-server in nested clusters
- **📈 Web Dashboard**: Visual interface to monitor and manage all KloneClusters
- **⚙️ Declarative Configuration**: Full CRD-based management with status tracking
- **🔄 Automatic Cleanup**: Graceful deletion with finalizers ensuring no orphaned resources

## 📋 Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration Reference](#configuration-reference)
- [Usage Examples](#usage-examples)
- [Architecture](#architecture)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## Prerequisites

### Required Components

1. **Kubernetes Cluster** (v1.11.3 or later)
   - Minimum resources: 4 CPU cores, 8GB RAM per nested cluster
   - Storage provisioner (e.g., `local-path`, `gp3`, etc.)
   - Admin access to install CRDs and RBAC resources

2. **kubectl** (v1.11.3 or later)
   ```bash
   kubectl version --client
   ```

3. **Container Runtime**
   - Docker (17.03+), Podman, or containerd
   - Required only if building operator image from source

4. **Go** (1.25.3 or later) - _Optional, for development only_
   ```bash
   go version
   ```

### Optional Components

Depending on your ingress configuration:

- **Tailscale Operator**: Required if using `ingress.type: tailscale`
  ```bash
  # Install Tailscale Operator
  kubectl apply -f https://github.com/tailscale/tailscale/releases/latest/download/operator.yaml
  ```

- **AWS Load Balancer Controller**: Required if using `ingress.type: loadbalancer`
  ```bash
  # See: https://kubernetes-sigs.github.io/aws-load-balancer-controller/
  helm repo add eks https://aws.github.io/eks-charts
  helm install aws-load-balancer-controller eks/aws-load-balancer-controller \
    -n kube-system \
    --set clusterName=<your-cluster-name>
  ```

## Installation

### Method 1: Deploy Pre-built Operator (Recommended)

This method uses the pre-built operator image from Docker Hub.

#### Step 1: Clone the Repository

```bash
git clone https://github.com/Raghavendiran-2002/klone.git
cd klone
```

#### Step 2: Install CRDs

Install the Custom Resource Definitions into your cluster:

```bash
make install
```

This creates the `KloneCluster` CRD in your cluster. Verify installation:

```bash
kubectl get crd kloneclusters.klone.klone.io
```

#### Step 3: Deploy the Operator

Deploy the operator controller manager:

```bash
IMG=raghavendiran2002/klone-operator:v1.0.24 make deploy
```

This will:
- Create the `operator-system` namespace
- Deploy the controller manager
- Set up necessary RBAC roles and service accounts

#### Step 4: Verify Installation

Check that the operator is running:

```bash
kubectl get pods -n operator-system

# Expected output:
# NAME                                          READY   STATUS    RESTARTS   AGE
# operator-controller-manager-xxxxxxxxx-xxxxx   2/2     Running   0          1m
```

View operator logs:

```bash
kubectl logs -n operator-system -l control-plane=controller-manager -f
```

#### Step 5: Deploy Dashboard (Optional)

Deploy the web dashboard to visualize your clusters:

```bash
kubectl apply -f config/dashboard/deployment.yaml
```

Access the dashboard:

```bash
kubectl port-forward -n operator-system svc/klone-dashboard 8080:8080
# Open http://localhost:8080 in your browser
```

### Method 2: Build and Deploy from Source

#### Step 1: Clone and Build

```bash
git clone https://github.com/Raghavendiran-2002/klone.git
cd klone-operator

# Build the operator binary
make build

# Build Docker image (replace with your registry)
export IMG=raghavendiran2002/klone-operator:v1.0.0
make docker-build

# Push to your registry
make docker-push
```

#### Step 2: Install CRDs and Deploy

```bash
# Install CRDs
make install

# Deploy operator
make deploy IMG=raghavendiran2002/klone-operator:v1.0.0
```

## Quick Start

### Create Your First Nested Cluster

#### 1. Basic Cluster (No Ingress)

Create a minimal cluster for testing:

```yaml
# examples/test-cluster.yaml
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: test-cluster
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: supersecrettoken123
    controlPlane:
      replicas: 1
    worker:
      replicas: 1

  storage:
    storageClass: local-path
    size: 2Gi
    hostPath: /tmp/klone

  terminal:
    image: alpine:3.19
    replicas: 1

  ingress:
    type: none

  metricsServer:
    enabled: false
```

Apply the configuration:

```bash
kubectl apply -f examples/test-cluster.yaml
```

#### 2. Monitor Cluster Creation

Watch the cluster being created:

```bash
# Watch the KloneCluster status
kubectl get klonecluster test-cluster -w

# Check all resources in the cluster's namespace
kubectl get all -n test-cluster

# View operator logs
kubectl logs -n operator-system -l control-plane=controller-manager -f
```

#### 3. Access the Nested Cluster

Once the cluster is ready (Status: Running), access it via terminal pod:

```bash
# Port-forward to the terminal
kubectl port-forward -n test-cluster svc/klone-terminal 7681:7681

# Open http://localhost:7681 in your browser
# You now have a web shell with kubectl configured for the nested cluster!
```

Or access directly via kubectl exec:

```bash
# Get a shell in the terminal pod
kubectl exec -it -n test-cluster deployment/klone-terminal -- sh

# Inside the pod, kubectl is already configured
kubectl get nodes
kubectl get pods -A
```

#### 4. Verify the Nested Cluster

Inside the terminal, verify the nested k3s cluster is working:

```bash
# Check nodes
kubectl get nodes

# Create a test deployment in the nested cluster
kubectl create deployment nginx --image=nginx
kubectl get pods

# Check metrics (if enabled)
kubectl top nodes
```

#### 5. Clean Up

Delete the cluster when done:

```bash
kubectl delete klonecluster test-cluster
```

The operator will automatically clean up all resources (namespace, PV, ingress, etc.).

## Configuration Reference

### KloneCluster Spec

#### k3s Configuration

```yaml
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1  # k3s container image
    token: supersecrettoken123        # Shared secret for k3s authentication
    controlPlane:
      replicas: 1                      # Number of control plane instances (default: 1)
      resources:                       # Optional resource limits
        requests:
          cpu: "500m"
          memory: "512Mi"
        limits:
          cpu: "2000m"
          memory: "1Gi"
    worker:
      replicas: 2                      # Number of worker nodes (default: 2)
      resources:                       # Optional resource limits
        requests:
          cpu: "200m"
          memory: "256Mi"
```

#### Storage Configuration

```yaml
spec:
  storage:
    storageClass: local-path           # StorageClass for PVC (default: local-path)
    size: 5Gi                          # PV size (default: 5Gi)
    hostPath: /home/user/klone        # Base directory for cluster data (default: /home/raghav/klone)
    nodeAffinity:                      # Optional node affinity for PV
      enabled: false                   # Enable node affinity (default: false)
      label: primary                   # Node label for affinity (default: primary)
```

#### Terminal Configuration

```yaml
spec:
  terminal:
    image: alpine:3.19                 # Terminal container image (default: alpine:3.19)
    replicas: 1                        # Number of terminal instances (default: 1)
    resources:                         # Optional resource limits
      requests:
        cpu: "100m"
        memory: "128Mi"
```

#### Ingress Configuration

##### No Ingress (Local Access Only)

```yaml
spec:
  ingress:
    type: none
```

##### Tailscale Ingress

```yaml
spec:
  ingress:
    type: tailscale
    tailscale:
      domain: your-tailnet.ts.net      # Your Tailscale network domain
      tags:                             # Optional Tailscale tags
        - tag:k8s-operator
        - tag:k8s
      annotations:                      # Optional custom annotations
        tailscale.com/expose: "true"
```

##### AWS Application Load Balancer

```yaml
spec:
  ingress:
    type: loadbalancer
    loadBalancer:
      scheme: internet-facing           # or 'internal' (default: internet-facing)
      certificateArn: arn:aws:acm:us-east-1:123456789012:certificate/abcd-1234
      externalDNS:                      # Optional external-dns configuration
        hostname: cluster.example.com   # DNS hostname
        ttl: 300                        # DNS TTL (default: 300)
      annotations:                      # Optional ALB annotations
        alb.ingress.kubernetes.io/target-type: ip
        alb.ingress.kubernetes.io/listen-ports: '[{"HTTPS":443}]'
```

#### Networking Configuration

```yaml
spec:
  networking:
    clusterCIDR: ""                    # Leave empty for auto-generation
    serviceCIDR: ""                    # Leave empty for auto-generation
```

**Note**: CIDRs are automatically generated based on cluster name to prevent conflicts. Manual specification is rarely needed.

#### Metrics Server Configuration

```yaml
spec:
  metricsServer:
    enabled: true                      # Auto-install metrics-server (default: true)
    image: registry.k8s.io/metrics-server/metrics-server:v0.7.0
```

## Usage Examples

### Example 1: Development Cluster with Tailscale

Perfect for development with secure remote access:

```yaml
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: dev-cluster
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: supersecrettoken123
    controlPlane:
      replicas: 1
    worker:
      replicas: 2

  storage:
    storageClass: local-path
    size: 5Gi
    hostPath: /home/raghav/klone

  terminal:
    image: alpine:3.19
    replicas: 1

  ingress:
    type: tailscale
    tailscale:
      domain: taile3ca5.ts.net
      tags:
        - tag:k8s-operator

  metricsServer:
    enabled: true
```

### Example 2: Production Cluster with AWS ALB

Enterprise-grade setup with load balancer and DNS:

```yaml
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: prod-cluster
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: supersecrettoken123
    controlPlane:
      replicas: 1
      resources:
        requests:
          cpu: "1000m"
          memory: "1Gi"
        limits:
          cpu: "4000m"
          memory: "4Gi"
    worker:
      replicas: 3
      resources:
        requests:
          cpu: "500m"
          memory: "512Mi"

  storage:
    storageClass: gp3
    size: 10Gi
    hostPath: /mnt/klone

  terminal:
    image: alpine:3.19
    replicas: 1

  ingress:
    type: loadbalancer
    loadBalancer:
      scheme: internet-facing
      certificateArn: arn:aws:acm:us-east-1:123456789012:certificate/abcd-1234
      externalDNS:
        hostname: prod-cluster.example.com
        ttl: 300
      annotations:
        alb.ingress.kubernetes.io/target-type: ip
        alb.ingress.kubernetes.io/listen-ports: '[{"HTTPS":443}]'

  metricsServer:
    enabled: true
```

### Example 3: Multi-Cluster Setup

Create multiple isolated clusters for different teams:

```bash
# Team A cluster
kubectl apply -f - <<EOF
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: team-a-cluster
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: teamAtoken
    controlPlane:
      replicas: 1
    worker:
      replicas: 2
  storage:
    storageClass: local-path
    size: 5Gi
  terminal:
    replicas: 1
  ingress:
    type: none
  metricsServer:
    enabled: true
EOF

# Team B cluster
kubectl apply -f - <<EOF
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: team-b-cluster
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: teamBtoken
    controlPlane:
      replicas: 1
    worker:
      replicas: 2
  storage:
    storageClass: local-path
    size: 5Gi
  terminal:
    replicas: 1
  ingress:
    type: none
  metricsServer:
    enabled: true
EOF

# List all clusters
kubectl get klonecluster
```

## Architecture

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                      Parent Kubernetes Cluster                   │
│                                                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │               Operator System Namespace                    │  │
│  │  ┌─────────────────────┐    ┌───────────────────────┐    │  │
│  │  │  Klone Operator     │    │   Web Dashboard        │    │  │
│  │  │  Controller Manager │    │   (Optional)           │    │  │
│  │  └─────────────────────┘    └───────────────────────┘    │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │            Nested Cluster Namespace (per cluster)          │  │
│  │                                                             │  │
│  │  ┌──────────────────┐      ┌───────────────────┐         │  │
│  │  │ k3s Control Plane│◄────►│  k3s Worker Nodes │         │  │
│  │  │  (StatefulSet)   │      │   (Deployment)    │         │  │
│  │  └──────────────────┘      └───────────────────┘         │  │
│  │          │                                                 │  │
│  │          ▼                                                 │  │
│  │  ┌──────────────────┐      ┌───────────────────┐         │  │
│  │  │ Web Terminal     │      │  Persistent       │         │  │
│  │  │  (Deployment)    │      │  Volume           │         │  │
│  │  └──────────────────┘      └───────────────────┘         │  │
│  │          │                                                 │  │
│  │          ▼                                                 │  │
│  │  ┌──────────────────┐                                     │  │
│  │  │   Ingress        │  (Tailscale/ALB/None)              │  │
│  │  └──────────────────┘                                     │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

1. **Klone Operator**: Watches `KloneCluster` CRs and reconciles desired state
2. **Control Plane**: k3s server running in StatefulSet with persistent storage
3. **Worker Nodes**: k3s agents connecting to control plane
4. **Terminal**: Web-based shell with kubectl configured for nested cluster access
5. **Ingress**: Routes external traffic to terminal (optional)
6. **Metrics Server**: Installed via Job in nested cluster for resource monitoring

### CIDR Allocation

To prevent network conflicts between multiple nested clusters:

- Each cluster gets unique CIDRs based on a hash of the cluster name
- Default ranges: `10.X.0.0/16` where X is derived from cluster name
- CIDRs are stored in `status.clusterCIDR` and `status.serviceCIDR`
- Manual CIDR specification is supported but rarely needed

## Development

### Prerequisites for Development

- Go 1.25.3+
- Docker or Podman
- kubectl
- Access to a Kubernetes cluster (Minikube, Kind, or remote)
- kubebuilder CLI (optional, for scaffolding)

### Local Development Workflow

#### 1. Clone the Repository

```bash
git clone https://github.com/Raghavendiran-2002/klone.git
cd klone
```

#### 2. Install Dependencies

```bash
go mod download
```

#### 3. Run Tests

```bash
# Run unit tests
make test

# Run linter
make lint

# Run e2e tests (creates a Kind cluster)
make test-e2e
```

#### 4. Run Operator Locally

Run the operator on your local machine (connects to your current kubeconfig cluster):

```bash
# Install CRDs
make install

# Run controller locally
make run
```

In another terminal, create a test cluster:

```bash
kubectl apply -f test-klonecluster.yaml
kubectl get klonecluster -w
```

#### 5. Make Changes and Rebuild

After modifying code:

```bash
# Regenerate CRDs and RBAC if you changed types or markers
make manifests generate

# Format code
make fmt

# Run tests
make test

# Build binary
make build
```

#### 6. Build and Deploy Custom Image

```bash
# Build Docker image
export IMG=raghavendiran2002/klone-operator:dev
make docker-build

# Push to registry
make docker-push

# Deploy to cluster
make deploy IMG=$IMG
```

### Project Structure

```
klone-operator/
├── .github/                    # GitHub workflows and templates
│   ├── workflows/             # CI/CD workflows
│   └── ISSUE_TEMPLATE/        # Issue and PR templates
├── api/v1alpha1/              # CRD API definitions
│   └── klonecluster_types.go  # KloneCluster spec and status
├── cmd/                       # Entry points
│   └── main.go                # Operator entry point
├── config/                    # Kubernetes manifests
│   ├── crd/                   # Generated CRDs (DO NOT EDIT)
│   ├── rbac/                  # RBAC roles and bindings
│   ├── manager/               # Operator deployment
│   ├── samples/               # Example KloneClusters
│   └── dashboard/             # Dashboard deployment
├── dashboard/                 # Web dashboard application
│   └── main.go
├── docs/                      # Additional documentation
│   ├── AGENTS.md              # Kubebuilder agent guide
│   ├── IMPLEMENTATION_PROGRESS.md
│   └── operator-guide.md      # Detailed operator guide
├── examples/                  # Sample manifests
│   ├── test-cluster.yaml
│   ├── klone_v1alpha1_klonecluster.yaml
│   └── klone_v1alpha1_klonecluster_alb.yaml
├── internal/controller/       # Controller implementation
│   ├── klonecluster_controller.go  # Main reconciliation logic
│   ├── workloads.go           # StatefulSet/Deployment creation
│   ├── resources.go           # Namespace/PV/PVC/ConfigMap creation
│   ├── ingress.go             # Ingress resource management
│   ├── cleanup.go             # Finalizer and deletion logic
│   ├── restart.go             # ConfigMap change handling
│   └── utils.go               # Helper functions, CIDR allocation
├── test/                      # Unit and E2E tests
├── CLAUDE.md                  # AI assistant reference guide
├── CONTRIBUTING.md            # Contribution guidelines
├── Dockerfile                 # Operator container image
├── LICENSE                    # Apache 2.0 License
├── Makefile                   # Build and deployment targets
├── PROJECT                    # Kubebuilder metadata
└── README.md                  # This file
```

### Common Development Tasks

#### Adding a New Field to the CRD

1. Edit `api/v1alpha1/klonecluster_types.go`
2. Add kubebuilder validation markers
3. Regenerate CRDs and code:
   ```bash
   make manifests generate
   ```
4. Update controller logic in `internal/controller/`
5. Run tests:
   ```bash
   make test
   ```

#### Debugging the Operator

```bash
# Run operator locally with verbose logging
make run

# Or deploy and check logs
kubectl logs -n operator-system -l control-plane=controller-manager -f

# Check CRD status
kubectl get klonecluster <name> -o yaml

# Check events
kubectl get events -n <cluster-namespace> --sort-by='.lastTimestamp'
```

## Troubleshooting

### Common Issues

#### 1. CRD Installation Fails

**Symptom**: `make install` fails with validation errors

**Solution**:
```bash
# Clean up old CRDs
make uninstall

# Reinstall
make install

# Verify
kubectl get crd kloneclusters.klone.klone.io
```

#### 2. Operator Pod CrashLooping

**Symptom**: Controller manager pod not starting

**Check**:
```bash
# View pod status
kubectl get pods -n operator-system

# Check logs
kubectl logs -n operator-system <pod-name>

# Check events
kubectl describe pod -n operator-system <pod-name>
```

**Common causes**:
- Missing RBAC permissions
- Image pull errors
- Invalid webhook configuration (if webhooks enabled)

#### 3. Nested Cluster kubectl Not Working

**Symptom**: Terminal pod can't connect to nested cluster

**Check**:
```bash
# Verify control plane is running
kubectl get pods -n <cluster-name> -l app=klone-controlplane

# Check Service endpoints
kubectl get endpoints -n <cluster-name> klone-controlplane

# Check kubeconfig in terminal pod
kubectl exec -it -n <cluster-name> deployment/klone-terminal -- cat /root/.kube/config

# Verify network connectivity
kubectl exec -it -n <cluster-name> deployment/klone-terminal -- wget -O- https://klone-controlplane:6443
```

**Solution**: The kubeconfig must use the Service DNS name (`klone-controlplane`) not `localhost`

#### 4. CIDR Conflicts

**Symptom**: Pods in nested cluster can't communicate or get IP addresses

**Check**:
```bash
# Check allocated CIDRs
kubectl get klonecluster <name> -o jsonpath='{.status.clusterCIDR}'
kubectl get klonecluster <name> -o jsonpath='{.status.serviceCIDR}'

# Verify k3s is using correct CIDRs
kubectl logs -n <cluster-name> statefulset/klone-controlplane | grep -i cidr
```

**Solution**: CIDRs are auto-generated. If conflicts occur, manually specify in spec:
```yaml
spec:
  networking:
    clusterCIDR: "10.100.0.0/16"
    serviceCIDR: "10.101.0.0/16"
```

#### 5. Ingress Not Accessible

**For Tailscale**:
- Verify Tailscale Operator is installed: `kubectl get pods -n tailscale`
- Check Ingress status: `kubectl get ingress -n <cluster-name>`
- View Tailscale operator logs: `kubectl logs -n tailscale -l app=operator`

**For ALB**:
- Verify AWS LB Controller is running: `kubectl get pods -n kube-system -l app.kubernetes.io/name=aws-load-balancer-controller`
- Check Ingress annotations: `kubectl describe ingress -n <cluster-name>`
- View ALB controller logs: `kubectl logs -n kube-system -l app.kubernetes.io/name=aws-load-balancer-controller`

#### 6. Resources Not Cleaned Up After Deletion

**Symptom**: Namespace or PV remains after deleting KloneCluster

**Check**:
```bash
# Check if finalizer is stuck
kubectl get klonecluster <name> -o yaml | grep finalizers

# Force remove finalizer if needed (use with caution)
kubectl patch klonecluster <name> -p '{"metadata":{"finalizers":[]}}' --type=merge

# Manually clean up resources
kubectl delete namespace <cluster-name>
kubectl delete pv klone-<cluster-name>-pv
```

#### 7. Metrics Server Not Working

**Symptom**: `kubectl top nodes` fails in nested cluster

**Check**:
```bash
# Check if metrics-server was installed
kubectl get klonecluster <name> -o jsonpath='{.status.metricsServerInstalled}'

# Check metrics-server job status
kubectl get job -n <cluster-name> install-metrics-server

# Check metrics-server in nested cluster (from terminal pod)
kubectl get deployment -n kube-system metrics-server
kubectl logs -n kube-system deployment/metrics-server
```

**Solution**: Metrics server requires proper TLS setup. k3s auto-generates this, but if issues persist:
```bash
# Manually install metrics-server in nested cluster
kubectl exec -it -n <cluster-name> deployment/klone-terminal -- sh
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

### Getting Help

- Check operator logs: `kubectl logs -n operator-system -l control-plane=controller-manager -f`
- View cluster status: `kubectl describe klonecluster <name>`
- Check events: `kubectl get events -n <cluster-namespace> --sort-by='.lastTimestamp'`
- Review CLAUDE.md for architecture details
- Open an issue on GitHub with logs and cluster YAML

## Uninstallation

### Remove All Clusters

Delete all KloneCluster resources:

```bash
kubectl delete klonecluster --all
```

Wait for all clusters to be deleted (finalizers will clean up resources):

```bash
kubectl get klonecluster -w
```

### Uninstall the Operator

Remove the operator and CRDs:

```bash
# Remove operator deployment
make undeploy

# Remove CRDs (this will delete all KloneCluster resources!)
make uninstall
```

**Warning**: Removing CRDs will delete all KloneCluster resources immediately!

### Clean Up Manually (if needed)

If automated cleanup fails:

```bash
# Delete all cluster namespaces
kubectl get ns -l klone.io/cluster -o name | xargs kubectl delete

# Delete PVs
kubectl get pv -l klone.io/cluster -o name | xargs kubectl delete

# Delete operator namespace
kubectl delete namespace operator-system
```

## Contributing

Contributions are welcome! Please follow these guidelines:

### Reporting Issues

- Use GitHub Issues to report bugs or request features
- Include operator logs, KloneCluster YAML, and error messages
- Specify Kubernetes version and environment details

### Submitting Pull Requests

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make changes following the project structure
4. Run tests: `make test lint`
5. Regenerate manifests if needed: `make manifests generate`
6. Commit with clear messages
7. Push and create a Pull Request

### Development Standards

- Follow Go best practices and conventions
- Add tests for new functionality
- Update documentation (README, CLAUDE.md)
- Use kubebuilder markers for CRD validation
- Maintain the existing file organization

## License

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

---

**Built with ❤️ using [Kubebuilder](https://book.kubebuilder.io/)**
