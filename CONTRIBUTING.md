# Contributing to Klone Operator

Thank you for your interest in contributing to Klone Operator! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [Community](#community)

## Code of Conduct

This project adheres to a code of conduct. By participating, you are expected to uphold this code. Please be respectful and constructive in all interactions.

## Getting Started

### Prerequisites

Before you begin, ensure you have:

- Go 1.25.3 or later
- Docker or Podman
- kubectl
- Access to a Kubernetes cluster (minikube, kind, or remote)
- Git

### Setting Up Your Development Environment

1. **Fork the repository** on GitHub

2. **Clone your fork**:
   ```bash
   git clone https://github.com/YOUR_USERNAME/klone-operator.git
   cd klone-operator
   ```

3. **Add upstream remote**:
   ```bash
   git remote add upstream https://github.com/ORIGINAL_OWNER/klone-operator.git
   ```

4. **Install dependencies**:
   ```bash
   go mod download
   ```

5. **Install CRDs** (optional, for local testing):
   ```bash
   make install
   ```

6. **Run tests** to verify setup:
   ```bash
   make test
   ```

## Development Workflow

### Creating a Feature Branch

Always create a new branch for your work:

```bash
git checkout -b feature/my-awesome-feature
```

Branch naming conventions:
- `feature/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation updates
- `refactor/` - Code refactoring
- `test/` - Test improvements

### Making Changes

1. **Write your code** following our [coding standards](#coding-standards)

2. **Add tests** for new functionality

3. **Run tests locally**:
   ```bash
   make test
   make lint
   ```

4. **Update documentation** if needed

5. **Regenerate manifests** if you modified CRD types:
   ```bash
   make manifests generate
   ```

### Committing Your Changes

Write clear, descriptive commit messages:

```bash
git commit -m "feat: add support for custom storage classes

- Add storageClassName field to KloneCluster spec
- Update reconciliation logic to use custom storage class
- Add validation for storage class existence
- Update documentation and examples

Fixes #123"
```

Commit message format:
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Test changes
- `chore:` - Build/tooling changes

### Syncing with Upstream

Keep your fork up to date:

```bash
git fetch upstream
git checkout main
git merge upstream/main
git push origin main
```

Rebase your feature branch:

```bash
git checkout feature/my-awesome-feature
git rebase main
```

## Pull Request Process

### Before Submitting

1. **Ensure all tests pass**:
   ```bash
   make test
   make lint
   ```

2. **Update documentation** (README, CLAUDE.md, etc.)

3. **Add examples** if introducing new features

4. **Test manually** in a real Kubernetes cluster

5. **Review your own code** - read through your changes critically

### Submitting the PR

1. **Push your branch** to your fork:
   ```bash
   git push origin feature/my-awesome-feature
   ```

2. **Open a Pull Request** on GitHub

3. **Fill out the PR template** completely

4. **Link related issues** using keywords like "Fixes #123"

5. **Request reviews** from maintainers

### PR Review Process

- Maintainers will review your PR within a few days
- Address review comments by pushing new commits
- Once approved, maintainers will merge your PR
- Your contribution will be included in the next release

### PR Requirements

Your PR must:
- [ ] Pass all CI checks (tests, linting)
- [ ] Include tests for new functionality
- [ ] Update relevant documentation
- [ ] Follow coding standards
- [ ] Have clear commit messages
- [ ] Be rebased on the latest main branch

## Coding Standards

### Go Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports` for formatting
- Run `make lint` and fix all issues
- Keep functions small and focused
- Add comments for exported functions and complex logic

### Kubebuilder Conventions

- **Never edit auto-generated files**:
  - `config/crd/bases/*.yaml`
  - `config/rbac/role.yaml`
  - `**/zz_generated.*.go`

- **Always use kubebuilder markers**:
  ```go
  // +kubebuilder:validation:Minimum=1
  // +kubebuilder:validation:Maximum=10
  Replicas int32 `json:"replicas,omitempty"`
  ```

- **Run code generation after changes**:
  ```bash
  make manifests generate
  ```

### File Organization

Follow the existing structure:

```
.
├── api/v1alpha1/           # CRD definitions
├── cmd/                    # Entry points
├── config/                 # Kubernetes manifests
├── dashboard/              # Dashboard application
├── docs/                   # Documentation
├── examples/               # Sample manifests
├── internal/controller/    # Controller logic
│   ├── *_controller.go    # Main reconciliation
│   ├── workloads.go       # Workload creation
│   ├── resources.go       # Resource creation
│   ├── ingress.go         # Ingress handling
│   ├── cleanup.go         # Finalizer logic
│   └── utils.go           # Helper functions
└── test/                   # Tests
```

### Controller Code Patterns

When adding controller logic:

1. **Check if resource exists**
2. **Create with owner reference** if missing
3. **Update status** to reflect actual state
4. **Handle errors** gracefully

Example:

```go
// Check if StatefulSet exists
existingSts := &appsv1.StatefulSet{}
err := r.Get(ctx, types.NamespacedName{
    Name:      "klone-controlplane",
    Namespace: cluster.Status.Namespace,
}, existingSts)

if err != nil && apierrors.IsNotFound(err) {
    // Create new StatefulSet
    sts := r.createControlPlaneStatefulSet(cluster)
    if err := controllerutil.SetControllerReference(cluster, sts, r.Scheme); err != nil {
        return ctrl.Result{}, err
    }
    if err := r.Create(ctx, sts); err != nil {
        return ctrl.Result{}, err
    }
}
```

## Testing Guidelines

### Unit Tests

- Write unit tests for all new functions
- Use table-driven tests where appropriate
- Mock external dependencies
- Aim for >80% code coverage

Example:

```go
func TestAllocateCIDRs(t *testing.T) {
    tests := []struct {
        name          string
        clusterName   string
        wantCluster   string
        wantService   string
    }{
        {
            name:        "test-cluster",
            clusterName: "test-cluster",
            wantCluster: "10.X.0.0/16",
            wantService: "10.Y.0.0/16",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cluster, service := AllocateCIDRs(tt.clusterName)
            // assertions...
        })
    }
}
```

### E2E Tests

- Add E2E tests for significant features
- Tests run in an isolated Kind cluster
- Clean up resources after tests

Run E2E tests:

```bash
make test-e2e
```

### Manual Testing

Before submitting, test manually:

1. Create a KloneCluster
2. Verify all components start correctly
3. Test kubectl access in nested cluster
4. Test ingress (if applicable)
5. Delete the cluster and verify cleanup

## Documentation

### When to Update Documentation

Update documentation when you:
- Add or modify CRD fields
- Change behavior or APIs
- Add new features
- Fix bugs that affect usage

### Documentation Files

- **README.md**: Main user-facing documentation
- **CLAUDE.md**: Developer/AI assistant reference
- **docs/**: Additional detailed guides
- **examples/**: Sample manifests with comments

### Writing Good Documentation

- Use clear, concise language
- Provide examples
- Explain the "why" not just the "what"
- Keep formatting consistent
- Test all code examples

## Community

### Getting Help

- **GitHub Issues**: Report bugs or request features
- **GitHub Discussions**: Ask questions and share ideas
- **Pull Request Comments**: Discuss specific code changes

### Asking Questions

When asking for help:
1. Search existing issues first
2. Provide context and details
3. Include error messages and logs
4. Share your environment details
5. Describe what you've already tried

### Reporting Bugs

Use the bug report template and include:
- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- Logs and error messages
- Environment details
- KloneCluster YAML

### Suggesting Features

Use the feature request template and explain:
- The problem you're trying to solve
- Your proposed solution
- Alternative approaches considered
- Whether you can contribute the implementation

## Recognition

All contributors will be recognized in:
- Release notes
- GitHub contributors page
- Project documentation (for significant contributions)

## Questions?

If you have questions about contributing, please open a GitHub issue or discussion. We're here to help!

---

**Thank you for contributing to Klone Operator!** 🎉
