# Klone Helm Charts

This repository hosts Helm charts for the Klone Operator.

## Usage

Add the Helm repository:

```bash
helm repo add klone https://raghavendiran-2002.github.io/klone/helm-charts
helm repo update
```

Install the Klone Operator:

```bash
helm install klone-operator klone/klone-operator \
  --namespace klone \
  --create-namespace
```

## Available Charts

- **klone-operator**: Kubernetes Operator for managing nested K3s clusters

## Documentation

Visit the [main repository](https://github.com/Raghavendiran-2002/klone) for complete documentation.
