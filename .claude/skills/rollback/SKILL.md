---
name: rollback
description: Rollback operator or dashboard to a previous version
disable-model-invocation: true
---

Revert the operator or dashboard to a previous version by updating manifest files and redeploying.

Usage: `/rollback <component> <version>`

**Arguments:**
- `component`: Which component to rollback
  - `operator` - Rollback operator only
  - `dashboard` - Rollback dashboard only

- `version`: Target version to rollback to (e.g., `v1.0.50`, `v1.2.3`)

**Examples:**
- `/rollback operator v1.0.50` - Rollback operator to v1.0.50
- `/rollback dashboard v1.0.11` - Rollback dashboard to v1.0.11

## Workflow

1. **Validate arguments**:
   - Check component is `operator` or `dashboard`
   - Check version format matches `vX.Y.Z`
   - If invalid: Report error and show usage

2. **Verify version exists**:
   - List git tags: `git tag -l`
   - Check if tag exists for the version:
     - For operator: Look for `v{VERSION}` or `operator-v{VERSION}`
     - For dashboard: Look for `dashboard-v{VERSION}`
   - If tag not found:
     - Warn: "Version {VERSION} not found in git tags"
     - Ask: "Continue anyway? (yes/no)"
     - If no: Exit

3. **For operator rollback**:

   a. Parse version (remove 'v' prefix): `v1.0.50` → `1.0.50`

   b. Update manifest files:
      - `helm/klone-operator/Chart.yaml`:
        - Line 5: `version: {VERSION}`
        - Line 6: `appVersion: "v{VERSION}"`
      - `helm/klone-operator/values.yaml`:
        - Line 13: `tag: v{VERSION}`
      - `config/manager/kustomization.yaml`:
        - Line 8: `newTag: v{VERSION}`

   c. Verify image exists in Docker Hub:
      - Run: `docker manifest inspect raghavendiran2002/klone-operator:v{VERSION}`
      - If fails: Warn "Image may not exist, but continuing"

   d. Deploy rolled-back version:
      - Run: `IMG=raghavendiran2002/klone-operator:v{VERSION} make deploy`
      - If fails: Report error and stop

   e. Verify rollback:
      - Check pod: `kubectl get pods -n klone -l control-plane=controller-manager`
      - Verify image: `kubectl get deployment -n klone -o jsonpath='{.spec.template.spec.containers[0].image}'`
      - Show logs (last 20 lines)

4. **For dashboard rollback**:

   a. Update manifest file:
      - `config/dashboard/deployment.yaml`:
        - Line 21: `image: raghavendiran2002/klone-dashboard:v{VERSION}`

   b. Verify image exists:
      - Run: `docker manifest inspect raghavendiran2002/klone-dashboard:v{VERSION}`
      - If fails: Warn but continue

   c. Deploy rolled-back version:
      - Run: `kubectl apply -f config/dashboard/deployment.yaml`
      - If fails: Report error and stop

   d. Verify rollback:
      - Check pod: `kubectl get pods -n klone -l app=klone-dashboard`
      - Verify image: `kubectl get deployment klone-dashboard -n klone -o jsonpath='{.spec.template.spec.containers[0].image}'`
      - Show logs (last 20 lines)

5. **Report completion**:
   ```
   ✓ Rollback completed successfully!

   Component: {component}
   Version: {OLD_VERSION} → {NEW_VERSION}

   Updated Files:
   {list of updated files}

   Deployment Status:
   {pod status}

   Image Verification:
   Current image: {actual image from deployment}

   Recent Logs:
   {last 20 lines of pod logs}

   Note: Changes are not committed to git.
   - To keep: /push "chore: Rollback {component} to {version}"
   - To discard: git checkout {files}
   ```

6. **Suggest next steps**:
   ```
   Next Steps:
   1. Verify functionality:
      - Check operator logs: kubectl logs -n klone -l control-plane=controller-manager -f
      - Test cluster creation: /test-cluster

   2. If rollback is successful:
      - Commit changes: /push "chore: Rollback {component} to {version}"
      - Or create a release with the reverted version

   3. If rollback didn't fix the issue:
      - Try a different version: /rollback {component} {version}
      - Or revert changes: git checkout {files}
   ```

## Version Discovery

To find available versions:
```bash
# List all release tags
git tag -l "v*" --sort=-version:refname

# List operator tags
git tag -l "operator-v*" --sort=-version:refname

# List dashboard tags
git tag -l "dashboard-v*" --sort=-version:refname

# List recent releases (last 10)
git tag -l "v*" --sort=-version:refname | head -10
```

## Error Handling

- **Invalid component**: Report valid options and exit
- **Invalid version format**: Must be `vX.Y.Z` format
- **Version tag not found**: Warn and ask to continue
- **Docker image not found**: Warn and ask to continue
- **Deployment fails**: Show kubectl error, suggest checking logs
- **No pod found after deployment**: Report issue and suggest manual verification

## Important Notes

- **Changes are NOT auto-committed** - You must manually commit or discard
- Rollback only updates manifest files and redeploys
- Does not affect git history or tags
- Docker images must exist in registry (cannot rollback to unbuilt versions)
- For operator, all 3 version files are updated for consistency
- After rollback, consider creating a new release if version should be permanent

## Emergency Rollback

If the operator is completely broken and can't recover:

1. **Quick rollback to last known good version**:
   ```
   /rollback operator v1.0.50
   ```

2. **Verify it's working**:
   ```
   kubectl logs -n klone -l control-plane=controller-manager --tail=50
   ```

3. **If still broken, try previous version**:
   ```
   /rollback operator v1.0.49
   ```

4. **Once stable, commit the rollback**:
   ```
   /push "chore: Emergency rollback to v1.0.50 due to {issue}"
   ```

## Comparison with git revert

**Rollback skill (recommended for deployed code)**:
- Updates manifest files to previous version
- Redeploys with older Docker image
- Doesn't change git history
- Changes are uncommitted (can test first)
- Fast recovery

**Git revert (for committed code changes)**:
- Creates new commit that undoes previous commit
- Affects git history
- Changes are immediately committed
- Better for source code bugs
- Requires rebuild and redeploy

## Use Cases

1. **Broken deployment**: New version has critical bug, need to restore service quickly
2. **Testing regression**: Verify if bug exists in older version
3. **Temporary fix**: Roll back while fixing issue in newer version
4. **Compatibility issue**: New version incompatible with environment
