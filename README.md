# Klone - Kubernetes Training Platform

A Kubernetes-in-Kubernetes training platform that runs nested k3s clusters for hands-on learning.

## Features

- Dynamic nested Kubernetes clusters
- Web-based terminal access
- Isolated training environments
- On-demand cluster provisioning

## Quick Start

```bash
# Deploy with Helm
helm install klone-training helm-chart --namespace klone --create-namespace

# Access the web interface
# Navigate to the Tailscale ingress URL
```

## Documentation

See [CLAUDE.md](CLAUDE.md) for detailed development and operational documentation.

## Architecture

- FastAPI cluster manager
- k3s nested clusters
- Tailscale networking
- ttyd web terminals

## Requirements

- Kubernetes 1.28+
- Tailscale operator
- metrics-server
- Node with `workload=primary` label
- Storage class: `local-path`
