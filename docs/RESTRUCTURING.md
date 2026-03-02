# Repository Restructuring

## Overview

The repository has been restructured to follow Kubernetes operator industry standards and best practices used by projects like cert-manager, prometheus-operator, and other CNCF operators.

## What Changed

### Before (Non-standard)

```
K8s-Training/
├── operator/               # All operator code nested in subdirectory (❌ Non-standard)
│   ├── api/
│   ├── cmd/
│   ├── config/
│   ├── internal/
│   ├── Makefile
│   ├── go.mod
│   └── README.md
└── test-klonecluster.yaml # Sample file at root
```

**Problems with old structure:**
- Operator code unnecessarily nested in `operator/` subdirectory
- Not following standard Kubernetes operator layout
- Harder to navigate and less intuitive for contributors
- Sample files scattered at root level
- Missing community standard files (LICENSE, CONTRIBUTING.md, etc.)

### After (Industry Standard)

```
klone/                     (repository root)
├── .github/               # ✅ GitHub-specific files
│   ├── workflows/         # CI/CD pipelines
│   ├── ISSUE_TEMPLATE/    # Issue templates
│   │   ├── bug_report.md
│   │   └── feature_request.md
│   └── pull_request_template.md
├── api/v1alpha1/          # ✅ CRD definitions at root
├── cmd/                   # ✅ Entry points at root
├── config/                # ✅ K8s manifests at root
│   ├── crd/
│   ├── rbac/
│   ├── samples/
│   ├── dashboard/
│   └── default/
├── dashboard/             # ✅ Dashboard code
├── docs/                  # ✅ All documentation
│   ├── AGENTS.md
│   ├── IMPLEMENTATION_PROGRESS.md
│   ├── operator-guide.md
│   └── RESTRUCTURING.md (this file)
├── examples/              # ✅ Sample manifests organized
│   ├── test-cluster.yaml
│   ├── klone_v1alpha1_klonecluster.yaml
│   └── klone_v1alpha1_klonecluster_alb.yaml
├── internal/controller/   # ✅ Controller logic at root
├── test/                  # ✅ Tests at root
├── CLAUDE.md              # ✅ AI assistant guide
├── CONTRIBUTING.md        # ✅ Contribution guidelines
├── Dockerfile             # ✅ Container build
├── LICENSE                # ✅ Apache 2.0 License
├── Makefile               # ✅ Build automation
├── PROJECT                # ✅ Kubebuilder metadata
├── README.md              # ✅ Main documentation
├── go.mod                 # ✅ Go dependencies
└── go.sum
```

**Benefits of new structure:**
- ✅ Follows Kubernetes operator standards (like cert-manager, prometheus-operator)
- ✅ All code at root level (standard for single-operator repositories)
- ✅ Clear separation: `/docs`, `/examples`, `/test`
- ✅ Includes all community standard files
- ✅ Easier for new contributors to understand and navigate
- ✅ Better GitHub integration with templates and workflows
- ✅ Professional open-source project appearance

## Detailed Changes

### 1. Moved Operator Code to Root

**Files moved from `operator/` to root:**
- `api/` → Root `api/`
- `cmd/` → Root `cmd/`
- `config/` → Root `config/`
- `dashboard/` → Root `dashboard/`
- `internal/` → Root `internal/`
- `test/` → Root `test/`
- `Dockerfile` → Root
- `Makefile` → Root
- `PROJECT` → Root
- `go.mod`, `go.sum` → Root
- Configuration files (`.gitignore`, `.golangci.yml`, etc.) → Root

### 2. Created `/examples` Directory

**Purpose:** Centralized location for sample manifests

**Contents:**
- `test-cluster.yaml` - Basic test cluster (moved from root)
- `klone_v1alpha1_klonecluster.yaml` - Development cluster example
- `klone_v1alpha1_klonecluster_alb.yaml` - Production cluster with ALB
- `kustomization.yaml` - Kustomize configuration

**Why:** Users expect to find examples in `/examples`, not scattered around

### 3. Created `/docs` Directory

**Purpose:** Centralized documentation

**Contents:**
- `AGENTS.md` - Kubebuilder agent/scaffolding guide
- `IMPLEMENTATION_PROGRESS.md` - Development progress tracking
- `operator-guide.md` - Original operator README (moved from `operator/README.md`)
- `RESTRUCTURING.md` - This file

**Why:** Additional documentation beyond README should be in `/docs`

### 4. Created `.github/` Structure

**Purpose:** GitHub-specific automation and templates

**Contents:**
- `workflows/` - CI/CD pipelines (moved from `operator/.github/workflows/`)
  - `lint.yml` - Linting workflow
  - `test.yml` - Unit tests workflow
  - `test-e2e.yml` - E2E tests workflow
- `ISSUE_TEMPLATE/` - Issue templates (new)
  - `bug_report.md` - Bug report template
  - `feature_request.md` - Feature request template
- `pull_request_template.md` - PR template (new)

**Why:** Better contributor experience with templates

### 5. Created Community Files

#### LICENSE (Apache 2.0)
Full Apache 2.0 license text with proper copyright attribution

#### CONTRIBUTING.md
Comprehensive contribution guide including:
- Development setup
- Workflow and branching
- Pull request process
- Coding standards
- Testing guidelines
- Documentation requirements

**Why:** Essential for open-source projects

### 6. Updated Documentation

#### README.md
- Updated all paths from `operator/` to root
- Fixed clone commands: `cd klone` instead of `cd klone-operator/operator`
- Updated GitHub URLs to `https://github.com/Raghavendiran-2002/klone`
- Updated Docker images to `raghavendiran2002/klone-operator` and `raghavendiran2002/klone-dashboard`
- Updated project structure diagram
- Added examples references: `examples/test-cluster.yaml`

#### CLAUDE.md
- Updated project structure diagram
- Fixed command paths
- Updated directory references

## Migration Guide

### For Existing Clones

If you have an existing clone of the repository:

```bash
# Navigate to your local clone
cd /path/to/your/klone

# Pull the latest changes
git fetch origin
git checkout main
git pull origin main

# Note: Working directory is now at root, not operator/
# Old: cd operator && make build
# New: make build

# Update your IDE/editor workspace if needed
```

### For New Clones

```bash
# Clone the repository
git clone https://github.com/Raghavendiran-2002/klone.git
cd klone

# Everything is at root now
make test
make build
kubectl apply -f examples/test-cluster.yaml
```

### For CI/CD Pipelines

Update any CI/CD scripts that referenced `operator/`:

**Before:**
```bash
cd operator
make test
```

**After:**
```bash
make test
```

### For Documentation Links

Update any bookmarks or links:

**Before:**
- `operator/README.md`
- `operator/config/samples/`
- Root level sample files

**After:**
- `README.md` (at root)
- `examples/` (for samples)
- `docs/` (for additional docs)

## Breaking Changes

⚠️ **Important:** The following paths have changed:

| Old Path | New Path |
|----------|----------|
| `operator/Makefile` | `Makefile` |
| `operator/api/` | `api/` |
| `operator/cmd/` | `cmd/` |
| `operator/config/` | `config/` |
| `operator/internal/` | `internal/` |
| `operator/README.md` | `docs/operator-guide.md` |
| `test-klonecluster.yaml` | `examples/test-klonecluster.yaml` |
| `operator/.github/` | `.github/` |

## Benefits Summary

1. **Standards Compliance**: Matches industry-standard Kubernetes operator layout
2. **Better Navigation**: Clearer structure for contributors
3. **Professional Appearance**: Looks like a mature open-source project
4. **Improved DX**: Better developer experience with templates and guides
5. **Community Ready**: All expected community files present
6. **Easier Maintenance**: Logical organization makes updates easier
7. **Better Discovery**: Examples and docs in expected locations

## References

Similar structures used by:
- [cert-manager](https://github.com/cert-manager/cert-manager)
- [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
- [external-dns](https://github.com/kubernetes-sigs/external-dns)
- [cluster-api](https://github.com/kubernetes-sigs/cluster-api)

## Questions?

If you have questions about the restructuring, please:
1. Check this document first
2. Review the updated README.md
3. Open a GitHub issue if still unclear

---

**Restructured on:** March 2, 2026
**Reason:** Align with Kubernetes operator industry standards
