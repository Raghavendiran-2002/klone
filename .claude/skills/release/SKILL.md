---
name: release
description: Create a new versioned release with changelog and GitHub Release
disable-model-invocation: true
---

Create a new release of the Klone Operator by analyzing changes, incrementing version, building images, generating changelog, creating git tag, and publishing GitHub Release.

## Workflow

1. **Ensure clean git state**:
   - Run: `git status`
   - If there are uncommitted changes:
     - Report: "Cannot create release with uncommitted changes"
     - Suggest: "Use /push to commit changes first, or stash them"
     - Exit

2. **Get latest tag and commits**:
   - Run: `git describe --tags --abbrev=0` to get last tag
   - If no tags exist: Use "v1.0.0" as starting point
   - Run: `git log {last-tag}..HEAD --oneline` to get commits since last tag
   - If no commits since last tag:
     - Report: "No commits since last release {last-tag}"
     - Exit

3. **Analyze changes and suggest version increment**:
   Categorize commits by type (look for conventional commit prefixes):

   - **MAJOR bump (X.0.0)** - Breaking changes:
     - Commits with `BREAKING CHANGE:` in body
     - Major API changes
     - Removal of features
     - Example: v1.2.5 → v2.0.0

   - **MINOR bump (x.Y.0)** - New features:
     - Commits starting with `feat:`
     - New CRD fields
     - New functionality (ArgoCD, ingress types)
     - Example: v1.2.5 → v1.3.0

   - **PATCH bump (x.y.Z)** - Bug fixes and minor updates:
     - Commits starting with `fix:`, `perf:`, `refactor:`, `docs:`, `chore:`
     - Bug fixes, performance improvements
     - Documentation, build changes
     - Example: v1.2.5 → v1.2.6

   **Analysis report:**
   ```
   Last release: v1.2.5
   Commits since then: 12

   Changes:
   - 3 features (feat:)
   - 2 bug fixes (fix:)
   - 4 performance improvements (perf:)
   - 3 chore/docs updates

   Suggested increment: MINOR (v1.3.0)
   Reason: New features added without breaking changes
   ```

4. **Ask for version confirmation**:
   - Prompt: "Release as v{MAJOR}.{MINOR}.{PATCH}? (yes/no/custom)"
   - If "yes": Use suggested version
   - If "no": Exit without creating release
   - If "custom": Ask "Enter version (format: v1.2.3): "

5. **Update all version files**:
   Update these 4 files with the new version:

   a. `helm/klone-operator/Chart.yaml`:
      - Line 5: `version: {VERSION}` (without 'v', e.g., "1.3.0")
      - Line 6: `appVersion: "v{VERSION}"` (with 'v', e.g., "v1.3.0")

   b. `helm/klone-operator/values.yaml`:
      - Line 13: `tag: v{VERSION}` (with 'v')

   c. `config/manager/kustomization.yaml`:
      - Line 8: `newTag: v{VERSION}` (with 'v')

   d. `config/dashboard/deployment.yaml`:
      - Line 21: Update image tag to match latest dashboard version
      - Read current dashboard version from this file
      - Keep dashboard version independent (don't auto-increment)

6. **Build and push operator image**:
   - Run: `IMG=raghavendiran2002/klone-operator:v{VERSION} make docker-build`
   - This builds multi-arch and pushes automatically
   - If build fails: Stop and report error

7. **Build and push dashboard image**:
   - Get current dashboard version from `config/dashboard/deployment.yaml`
   - Run: `docker buildx build --platform linux/amd64,linux/arm64 -t raghavendiran2002/klone-dashboard:v{DASHBOARD_VERSION} -f dashboard/Dockerfile --push .`
   - If build fails: Warn but continue (dashboard is optional)

8. **Generate changelog**:
   - Parse commits since last tag
   - Group by type: Features, Bug Fixes, Performance, Documentation, Other
   - Format as markdown:

   ```markdown
   ## v{VERSION} (YYYY-MM-DD)

   ### Features
   - feat(scope): Subject line from commit
   - feat(scope): Another feature

   ### Bug Fixes
   - fix(scope): Fixed something
   - fix(scope): Another fix

   ### Performance Improvements
   - perf(scope): Optimized something

   ### Documentation
   - docs(scope): Updated docs

   ### Maintenance
   - chore(scope): Build improvement
   - refactor(scope): Code cleanup
   ```

9. **Commit version updates**:
   - Stage version files: `git add helm/klone-operator/Chart.yaml helm/klone-operator/values.yaml config/manager/kustomization.yaml`
   - Set git user: `git config user.email "raghavendiran46461@gmail.com"`
   - Commit: `git commit -m "chore(release): Bump version to v{VERSION}"`
   - **NEVER** add Claude co-authorship

10. **Create git tag**:
    - Run: `git tag -a v{VERSION} -m "Release v{VERSION}"`
    - Annotated tag with release message

11. **Push changes and tag**:
    - Push commits: `git push`
    - Push tag: `git push --tags`
    - If push fails: Report error and suggest resolution

12. **Create GitHub Release**:
    - Run: `gh release create v{VERSION} --title "v{VERSION}" --notes "{changelog}" --latest`
    - This creates a GitHub Release with auto-generated changelog
    - Marks it as the latest release
    - If `gh` command not available: Skip and report manual steps

13. **Report completion**:
    ```
    ✓ Release v{VERSION} created successfully!

    Changes:
    - {N} commits since v{LAST_VERSION}
    - Built operator image: raghavendiran2002/klone-operator:v{VERSION}
    - Built dashboard image: raghavendiran2002/klone-dashboard:v{DASHBOARD_VERSION}

    Updated Files:
    - helm/klone-operator/Chart.yaml
    - helm/klone-operator/values.yaml
    - config/manager/kustomization.yaml

    Git:
    - Created tag: v{VERSION}
    - Pushed to remote

    GitHub Release:
    - URL: https://github.com/Raghavendiran-2002/klone/releases/tag/v{VERSION}

    Changelog:
    {generated changelog}

    Next Steps:
    - The Helm chart will be automatically published by CI/CD
    - Monitor GitHub Actions for Helm release workflow
    ```

## Version Increment Decision Tree

```
Has BREAKING CHANGE in commits?
├─ Yes → MAJOR bump (2.0.0)
└─ No
   ├─ Has feat: commits?
   │  └─ Yes → MINOR bump (1.3.0)
   └─ No
      └─ Has fix:/perf:/other commits?
         └─ Yes → PATCH bump (1.2.6)
```

## Error Handling

- **Uncommitted changes**: Stop and suggest committing or stashing
- **No commits since last tag**: Exit cleanly
- **Build failures**: Stop before creating tag
- **Push failures**: Report git error and suggest resolution
- **`gh` command not found**: Skip GitHub Release, provide manual instructions

## Important Notes

- **Helm chart version = operator version** (synchronized)
- Dashboard has independent versioning (not auto-incremented)
- Always use conventional commit format for accurate changelog generation
- The workflow triggers CI/CD to publish Helm chart automatically
- Multi-arch builds ensure compatibility with amd64 and arm64
- Git tag format: `v1.2.3` (with 'v' prefix)
- Chart version format: `1.2.3` (without 'v' in Chart.yaml version, with 'v' in appVersion)

## Manual GitHub Release (if `gh` unavailable)

If GitHub CLI is not installed:
1. Go to: https://github.com/Raghavendiran-2002/klone/releases/new
2. Tag: v{VERSION}
3. Title: v{VERSION}
4. Description: Paste the generated changelog
5. Check "Set as the latest release"
6. Click "Publish release"
