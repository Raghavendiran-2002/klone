# Klone Operator Helm Chart

This Helm chart deploys the Klone Operator on a Kubernetes cluster.

## What is Klone?

Klone is a Kubernetes Operator that enables you to deploy nested K3s clusters inside an existing Kubernetes cluster. Each KloneCluster creates an isolated k3s cluster with its own control plane, workers, and optional web terminal access.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+
- Cluster admin permissions

## Installation

### Install from Local Chart

If you have cloned the repository:

```bash
# From the repository root
helm install klone-operator ./helm/klone-operator --namespace klone --create-namespace
```

### Install from GitHub (Future - after packaging)

Once the chart is packaged and published:

```bash
# Add the repository (example - update with actual repository URL)
helm repo add klone https://raghavendiran-2002.github.io/klone/helm-charts
helm repo update

# Install the chart
helm install klone-operator klone/klone-operator --namespace klone --create-namespace
```

## Configuration

The following table lists the configurable parameters of the Klone Operator chart and their default values.

### Controller Manager

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controllerManager.image.repository` | Image repository | `raghavendiran2002/klone-operator` |
| `controllerManager.image.tag` | Image tag | `v1.0.51` |
| `controllerManager.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `controllerManager.replicaCount` | Number of replicas | `1` |
| `controllerManager.resources.limits.cpu` | CPU limit | `500m` |
| `controllerManager.resources.limits.memory` | Memory limit | `128Mi` |
| `controllerManager.resources.requests.cpu` | CPU request | `10m` |
| `controllerManager.resources.requests.memory` | Memory request | `64Mi` |
| `controllerManager.leaderElection.enabled` | Enable leader election | `true` |

### RBAC

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rbac.create` | Create RBAC resources | `true` |

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics service | `true` |
| `metrics.service.port` | Metrics service port | `8443` |
| `metrics.service.type` | Metrics service type | `ClusterIP` |

### CRDs

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs | `true` |
| `crds.keep` | Keep CRDs on chart uninstall | `true` |

## Usage Examples

### Basic Installation

```bash
helm install klone-operator ./helm/klone-operator --namespace klone --create-namespace
```

### Custom Values

Create a `values.yaml` file:

```yaml
controllerManager:
  image:
    tag: v1.0.52
  resources:
    limits:
      cpu: 1000m
      memory: 256Mi
```

Install with custom values:

```bash
helm install klone-operator ./helm/klone-operator -f values.yaml --namespace klone --create-namespace
```

### Upgrade

```bash
helm upgrade klone-operator ./helm/klone-operator --namespace klone
```

### Uninstall

```bash
helm uninstall klone-operator --namespace klone
```

**Note:** CRDs are not deleted by default when uninstalling. To delete them manually:

```bash
kubectl delete crd kloneclusters.klone.klone.io
```

## Creating a KloneCluster

After installing the operator, create a KloneCluster:

```yaml
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: my-cluster
spec:
  controlPlane:
    replicas: 1
  workers:
    replicas: 2
  terminal:
    enabled: true
  ingress:
    type: none
```

Apply it:

```bash
kubectl apply -f klonecluster.yaml
```

## Verification

Check the operator is running:

```bash
kubectl get pods -n klone
```

Check the KloneCluster status:

```bash
kubectl get klonecluster my-cluster -o yaml
```

## Troubleshooting

### View operator logs

```bash
kubectl logs -n klone -l control-plane=controller-manager -f
```

### Check CRD installation

```bash
kubectl get crd kloneclusters.klone.klone.io
```

### Validate Helm chart

```bash
# Lint the chart
helm lint ./helm/klone-operator

# Dry run installation
helm install klone-operator ./helm/klone-operator --namespace klone --create-namespace --dry-run
```

## Source Code

- GitHub: https://github.com/Raghavendiran-2002/klone
- Issues: https://github.com/Raghavendiran-2002/klone/issues

## License

Apache 2.0
