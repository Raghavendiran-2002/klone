---
name: Bug Report
about: Create a report to help us improve
title: '[BUG] '
labels: bug
assignees: ''

---

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**
Steps to reproduce the behavior:
1. Create KloneCluster with config '...'
2. Run command '...'
3. Observe error '...'

**Expected behavior**
A clear and concise description of what you expected to happen.

**KloneCluster YAML**
```yaml
# Paste your KloneCluster resource YAML here
```

**Logs**
```
# Paste operator logs here
kubectl logs -n operator-system -l control-plane=controller-manager
```

**Environment:**
- Kubernetes Version: [e.g., v1.28.0]
- Operator Version: [e.g., v1.0.24]
- Parent Cluster Platform: [e.g., EKS, GKE, minikube, kind]
- Ingress Type: [e.g., tailscale, loadbalancer, none]

**Additional context**
Add any other context about the problem here.
