---
name: bump-version
description: Update version numbers in manifest files without building or deploying
disable-model-invocation: true
---

Update version numbers in manifest files without building images or deploying. Useful for preparing releases or fixing version mismatches.

Usage: `/bump-version <component> <type>`

**Arguments:**
- `component`: Which component to version bump
  - `operator` - Bump operator version only
  - `dashboard` - Bump dashboard version only
  - `helm` - Bump Helm chart version (same as operator)
  - `all` - Bump both operator and dashboard

- `type`: Type of version increment
  - `major` - Bump major version (2.0.0)
  - `minor` - Bump minor version (1.3.0)
  - `patch` - Bump patch version (1.2.6)

**Examples:**
- `/bump-version operator patch` - Bump operator from v1.2.5 to v1.2.6
- `/bump-version dashboard minor` - Bump dashboard from v1.0.12 to v1.1.0
- `/bump-version all patch` - Bump both components' patch versions

## Workflow

1. **Validate arguments**:
   - Check that component is one of: operator, dashboard, helm, all
   - Check that type is one of: major, minor, patch
   - If invalid: Report error and show usage

2. **For operator/helm bump**:

   a. Read current version from `helm/klone-operator/Chart.yaml` line 5
   b. Parse version: `1.2.5` → {major: 1, minor: 2, patch: 5}
   c. Increment based on type:
      - major: 2.0.0
      - minor: 1.3.0
      - patch: 1.2.6
   d. Update these 3 files:
      - `helm/klone-operator/Chart.yaml`:
        - Line 5: `version: {VERSION}` (without 'v')
        - Line 6: `appVersion: "v{VERSION}"` (with 'v')
      - `helm/klone-operator/values.yaml`:
        - Line 13: `tag: v{VERSION}` (with 'v')
      - `config/manager/kustomization.yaml`:
        - Line 8: `newTag: v{VERSION}` (with 'v')
   e. Report: "Operator version updated: v{OLD} → v{NEW}"

3. **For dashboard bump**:

   a. Read current version from `config/dashboard/deployment.yaml` line 21
   b. Extract version from image tag: `raghavendiran2002/klone-dashboard:v1.0.12`
   c. Parse version: `1.0.12` → {major: 1, minor: 0, patch: 12}
   d. Increment based on type
   e. Update `config/dashboard/deployment.yaml`:
      - Line 21: `image: raghavendiran2002/klone-dashboard:v{VERSION}`
   f. Report: "Dashboard version updated: v{OLD} → v{NEW}"

4. **For all bump**:
   - Execute operator bump
   - Execute dashboard bump
   - Use the same increment type for both

5. **Report completion**:
   ```
   ✓ Version bump completed

   Component: {component}
   Type: {type}

   Updates:
   - Operator: v{OLD_OP} → v{NEW_OP}
   - Dashboard: v{OLD_DASH} → v{NEW_DASH}

   Updated Files:
   - helm/klone-operator/Chart.yaml
   - helm/klone-operator/values.yaml
   - config/manager/kustomization.yaml
   - config/dashboard/deployment.yaml

   Next Steps:
   - Review changes: git diff
   - Build images: /deploy-all
   - Or create release: /release
   ```

## Version Increment Logic

**Current version: 1.2.5**

- **major**: 1.2.5 → 2.0.0 (reset minor and patch to 0)
- **minor**: 1.2.5 → 1.3.0 (reset patch to 0)
- **patch**: 1.2.5 → 1.2.6 (increment patch only)

## Error Handling

- **Invalid component**: Report valid options and exit
- **Invalid type**: Report valid options and exit
- **Version file not found**: Report missing file path and exit
- **Cannot parse version**: Report current line content and exit

## Important Notes

- This skill ONLY updates manifest files, does not build or deploy
- Operator and Helm chart versions are always synchronized
- Dashboard version is independent and may differ from operator
- Version format: `v{MAJOR}.{MINOR}.{PATCH}` with 'v' prefix
- Changes are not automatically committed - use `/push` to commit

## Use Cases

1. **Prepare for release**: Bump versions before running `/release`
2. **Fix version mismatch**: Correct inconsistent versions across files
3. **Pre-build version update**: Update versions before manual build
4. **Testing**: Try different version numbers without deploying
