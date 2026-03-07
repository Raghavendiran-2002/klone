---
name: deploy-operator
description: Build and deploy the Klone Operator to the cluster
disable-model-invocation: true
---

Build, version-bump, and deploy the Klone Operator to the Kubernetes cluster.

## Workflow

1. **Ask about testing**:
   - Prompt: "Run tests before deploying? (yes/no)"
   - If yes: Run `make test`
   - If tests fail: Stop and report errors

2. **Check for API changes**:
   - Run `git diff --name-only` to see changed files
   - If `api/v1alpha1/` directory has changes:
     - Run `make manifests generate fmt`
     - Report: "Regenerated CRDs and code due to API changes"
   - Otherwise: Skip this step

3. **Get current version and increment**:
   - Read `helm/klone-operator/Chart.yaml`
   - Extract current version from line 5 (format: `version: 1.0.52`)
   - Increment patch version: v1.0.52 → v1.0.53
   - New version format: `v{MAJOR}.{MINOR}.{PATCH}` (with 'v' prefix for Docker)

4. **Build operator image**:
   - Run: `IMG=raghavendiran2002/klone-operator:v{VERSION} make docker-build`
   - This builds multi-arch (amd64 + arm64) and auto-pushes
   - If build fails:
     - Wait 5 seconds
     - Retry once with same command
     - If second failure: Stop and report detailed error

5. **Update version files**:
   Update these 3 files with the new version:

   a. `helm/klone-operator/Chart.yaml`:
      - Line 5: `version: {VERSION}` (without 'v', e.g., "1.0.53")
      - Line 6: `appVersion: "v{VERSION}"` (with 'v', e.g., "v1.0.53")

   b. `helm/klone-operator/values.yaml`:
      - Line 13: `tag: v{VERSION}` (with 'v')

   c. `config/manager/kustomization.yaml`:
      - Line 8: `newTag: v{VERSION}` (with 'v')

6. **Deploy operator**:
   - Run: `IMG=raghavendiran2002/klone-operator:v{VERSION} make deploy`
   - If deploy fails:
     - Wait 5 seconds
     - Retry once
     - If second failure: Stop and report error

7. **Verify deployment**:
   - Run: `kubectl get pods -n klone -l control-plane=controller-manager`
   - Check pod status (should be Running)
   - Get pod name and show recent logs (last 20 lines)

8. **Report completion**:
   ```
   ✓ Operator deployed successfully!

   Version: v{VERSION}
   Image: raghavendiran2002/klone-operator:v{VERSION}

   Pod Status:
   {pod status output}

   Recent Logs:
   {last 20 lines of logs}

   Updated Files:
   - helm/klone-operator/Chart.yaml
   - helm/klone-operator/values.yaml
   - config/manager/kustomization.yaml
   ```

## Error Handling

- **Test failures**: Show failed test output and stop
- **Build failures**: Show Docker error and stop after second attempt
- **Deploy failures**: Show kubectl error and stop after second attempt
- **Version file not found**: Report missing file and stop

## Important Notes

- Always use multi-arch builds (amd64 + arm64)
- Version format: Helm chart uses "1.0.53", Docker tags use "v1.0.53"
- The `make docker-build` target automatically pushes the image
- Deploy to `klone` namespace (operator-system in some older configs)
