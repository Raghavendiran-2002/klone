# Helm Chart Setup and GitHub Pages Deployment

## What Has Been Done

The Klone Operator Helm chart has been successfully created and packaged for distribution via GitHub Pages.

### Files Created

1. **Helm Chart** (`helm/klone-operator/`)
   - Chart.yaml - Chart metadata and version info
   - values.yaml - Configurable parameters
   - templates/ - Kubernetes manifest templates
   - README.md - Chart documentation

2. **Packaged Chart** (`helm/packages/`)
   - klone-operator-1.0.51.tgz - Packaged chart archive
   - index.yaml - Helm repository index

3. **GitHub Pages Content** (`docs/`)
   - index.html - Landing page with installation instructions
   - helm/packages/ - Packaged chart and index (copied for hosting)

## Next Steps: Enable GitHub Pages

To make the Helm chart available via `helm repo add`, you need to enable GitHub Pages:

### Option 1: Enable GitHub Pages via Repository Settings

1. Go to your GitHub repository: https://github.com/Raghavendiran-2002/klone
2. Click on **Settings** (tab at the top)
3. Scroll down to **Pages** section in the left sidebar
4. Under **Source**, select:
   - Branch: `main`
   - Folder: `/docs`
5. Click **Save**
6. Wait 1-2 minutes for deployment
7. GitHub will provide the URL: `https://raghavendiran-2002.github.io/klone/`

### Option 2: Verify GitHub Pages is Already Enabled

If you already have GitHub Pages enabled, verify the deployment:

```bash
curl -I https://raghavendiran-2002.github.io/klone/
```

If you get a 200 OK response, it's already live!

## Committing the Changes

You need to commit and push the new files to GitHub:

```bash
# Stage all the new Helm chart files
git add helm/ docs/

# Create a commit
git commit -m "Add Helm chart and GitHub Pages hosting

- Create Helm chart for Klone Operator v1.0.51
- Package chart and generate repository index
- Set up GitHub Pages with documentation
- Enable installation via: helm repo add klone https://raghavendiran-2002.github.io/klone/helm/packages/"

# Push to GitHub
git push origin main
```

## Using the Helm Repository

Once GitHub Pages is enabled and the changes are pushed, users can install the operator using:

### Add the Helm Repository

```bash
helm repo add klone https://raghavendiran-2002.github.io/klone/helm/packages/
helm repo update
```

### Install the Operator

```bash
# Install with default values
helm install klone-operator klone/klone-operator \
  --namespace klone \
  --create-namespace

# Or install with custom values
helm install klone-operator klone/klone-operator \
  --namespace klone \
  --create-namespace \
  --set controllerManager.image.tag=v1.0.51 \
  --set controllerManager.replicaCount=2
```

### Verify Installation

```bash
# Check operator pods
kubectl get pods -n klone

# View operator logs
kubectl logs -n klone -l control-plane=controller-manager -f

# Check Helm release
helm list -n klone
```

## Alternative: Local Installation

Users can also install directly from the cloned repository without using the Helm repository:

```bash
# Clone the repository
git clone https://github.com/Raghavendiran-2002/klone.git
cd klone

# Install from local chart
helm install klone-operator ./helm/klone-operator \
  --namespace klone \
  --create-namespace
```

## Updating the Chart

When you release a new version:

1. Update the version in `helm/klone-operator/Chart.yaml`:
   ```yaml
   version: 1.0.52
   appVersion: "v1.0.52"
   ```

2. Update `helm/klone-operator/values.yaml` if needed:
   ```yaml
   controllerManager:
     image:
       tag: v1.0.52
   ```

3. Package the new version:
   ```bash
   helm package helm/klone-operator -d helm/packages/
   ```

4. Update the repository index:
   ```bash
   helm repo index helm/packages/ --url https://raghavendiran-2002.github.io/klone/helm/packages/
   ```

5. Copy to docs directory:
   ```bash
   cp helm/packages/* docs/helm/packages/
   ```

6. Commit and push:
   ```bash
   git add helm/ docs/
   git commit -m "Release Helm chart v1.0.52"
   git push origin main
   ```

7. Users update their local repo:
   ```bash
   helm repo update
   helm upgrade klone-operator klone/klone-operator --namespace klone
   ```

## Verification Checklist

After enabling GitHub Pages and pushing changes:

- [ ] GitHub Pages is enabled in repository settings
- [ ] Changes are committed and pushed to GitHub
- [ ] Visit https://raghavendiran-2002.github.io/klone/ to see the landing page
- [ ] Visit https://raghavendiran-2002.github.io/klone/helm/packages/index.yaml to see the chart index
- [ ] Test adding the Helm repository: `helm repo add klone https://raghavendiran-2002.github.io/klone/helm/packages/`
- [ ] Search for the chart: `helm search repo klone`
- [ ] Install the chart: `helm install klone-operator klone/klone-operator -n klone --create-namespace`

## Directory Structure

```
klone/
├── helm/
│   ├── klone-operator/          # Helm chart source
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   ├── README.md
│   │   └── templates/
│   │       ├── _helpers.tpl
│   │       ├── deployment.yaml
│   │       ├── service.yaml
│   │       ├── serviceaccount.yaml
│   │       ├── namespace.yaml
│   │       ├── NOTES.txt
│   │       ├── crds/
│   │       │   └── klonecluster.yaml
│   │       └── rbac/
│   │           ├── clusterrole.yaml
│   │           └── clusterrolebinding.yaml
│   └── packages/                # Packaged charts (not in git)
│       ├── klone-operator-1.0.51.tgz
│       └── index.yaml
└── docs/                        # GitHub Pages content
    ├── index.html               # Landing page
    └── helm/
        └── packages/
            ├── klone-operator-1.0.51.tgz
            └── index.yaml
```

## GitHub Pages Benefits

GitHub Pages is **completely free** for public repositories and provides:

- HTTPS by default
- CDN distribution for fast downloads
- No bandwidth limits for reasonable use
- Automatic SSL certificates
- Simple deployment via git push

## Support

If users encounter issues:

1. Check GitHub Pages status: https://www.githubstatus.com/
2. Verify DNS propagation (may take a few minutes after enabling)
3. Clear Helm cache: `helm repo update`
4. Check repository visibility (must be public for free GitHub Pages)

## Documentation

Full chart documentation is available at:
- Landing page: https://raghavendiran-2002.github.io/klone/
- Chart README: helm/klone-operator/README.md
- Main README: README.md (update with Helm installation section)
