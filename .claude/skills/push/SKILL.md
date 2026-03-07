---
name: push
description: Create logical commits and push changes to remote
disable-model-invocation: true
---

Analyze code changes, create logical commits using conventional commit format, and push to the current branch.

If you provide an optional message argument (e.g., `/push "Add ArgoCD support"`), it will be used as the commit subject.

## Workflow

1. **Check git status**:
   - Run: `git status`
   - Identify all changed, staged, and untracked files
   - If no changes: Report "No changes to commit" and exit

2. **Run linting**:
   - Check if Go files were changed: Look for `*.go` files in git status
   - If Go files changed:
     - Run: `make lint`
     - If linting fails:
       - Show lint errors
       - Ask: "Fix linting issues? (yes/no/skip)"
       - If yes: Run `make lint-fix` to auto-fix, then re-run `make lint`
       - If no: Exit without committing
       - If skip: Continue with warning (not recommended)
   - If only non-Go files changed (markdown, YAML, etc.): Skip linting

3. **Analyze conversation history and changes**:
   - Review the conversation to understand what was done
   - Group changes by logical feature/fix (typically 1-3 commits)
   - For each group, determine:
     - Type: feat, fix, perf, refactor, docs, chore
     - Scope: Component affected (e.g., argocd, cidr, terminal, dashboard, build, release)
     - Subject: Clear, imperative description (50 chars max)
     - Body: Optional detailed explanation if changes are complex

4. **Determine commit type for each group**:
   - **feat**: New feature or capability
     - Examples: ArgoCD integration, new CRD field, new ingress type
   - **fix**: Bug fix
     - Examples: CIDR conflict resolution, kubeconfig server URL fix
   - **perf**: Performance improvement
     - Examples: Docker build optimization, faster reconciliation
   - **refactor**: Code restructuring without behavior change
     - Examples: Split large function, reorganize files
   - **docs**: Documentation only
     - Examples: README updates, comment improvements, CLAUDE.md updates
   - **chore**: Build, config, or tooling changes
     - Examples: Makefile updates, CI/CD changes, version bumps, dependency updates

5. **Create commits**:
   - For each logical group:
     a. Stage relevant files: `git add <files>`
     b. Set git user email: `git config user.email "raghavendiran46461@gmail.com"`
     c. Create commit with conventional format:
        ```
        <type>(<scope>): <subject>

        <optional body>
        ```
     d. **CRITICAL**: Never add `Co-Authored-By: Claude <noreply@anthropic.com>`

6. **Push to remote**:
   - Get current branch: `git branch --show-current`
   - Push: `git push origin <branch>`
   - If push fails (e.g., diverged history):
     - Report error
     - Suggest: `git pull --rebase` or force push if appropriate

7. **Report completion**:
   ```
   ✓ Created {N} commit(s) and pushed to {branch}

   Commits:
   1. {type}({scope}): {subject}
      Files: {changed files}

   2. {type}({scope}): {subject}
      Files: {changed files}

   Push result: {git push output}
   ```

## Commit Message Examples

**Good commit messages:**
```
feat(argocd): Add automatic cluster registration to host ArgoCD

Implements ArgoCD integration with:
- Auto-detection of ArgoCD in host cluster
- Cluster registration with configurable labels
- Repository secret import from host
- CRD installation in nested cluster

Closes #45
```

```
fix(terminal): Correct kubeconfig server URL to use Service DNS

Terminal pods were using localhost which failed when accessing
the k3s control plane. Updated to use the Service DNS name
(klone-controlplane) instead.
```

```
perf(build): Optimize Docker multi-stage builds

Reordered Dockerfile layers to maximize cache hits and reduce
build times by ~40% in CI/CD pipeline.
```

```
docs(readme): Update installation guide with Helm instructions

Added Helm installation as the primary method, moved
kustomize to alternative options.
```

```
chore(release): Bump version to v1.0.53

Updates operator and Helm chart versions.
```

## Grouping Strategy

**One commit per logical feature/fix** - Group related changes:

**Example 1: ArgoCD Feature**
- Changed files: `api/v1alpha1/klonecluster_types.go`, `internal/controller/klonecluster_controller.go`, `internal/controller/argocd.go`, `README.md`
- Single commit: `feat(argocd): Add ArgoCD integration with cluster registration`

**Example 2: Multiple Unrelated Changes**
- Group 1: API changes, controller logic → `feat(cidr): Implement dynamic CIDR allocation`
- Group 2: README updates → `docs(readme): Add CIDR allocation section`
- Group 3: Makefile changes → `chore(build): Add lint target to Makefile`

**Example 3: Version Bump**
- All version file updates → `chore(release): Bump version to v1.0.53`

## Error Handling

- **No changes**: Report "No changes to commit" and exit cleanly
- **Linting failures**: Show errors and offer to auto-fix or skip
- **Lint-fix failures**: If auto-fix doesn't resolve all issues, show remaining errors and exit
- **Push rejected**: Show git error and suggest resolution
- **Git config fails**: Report error and stop
- **Untracked sensitive files**: Warn if files like `.env` or `*credentials*` are staged

## Important Notes

- **Always** use email: `raghavendiran46461@gmail.com`
- **Never** add Claude co-authorship
- **Always** run linting before committing Go code changes
- Use imperative mood: "Add feature" not "Added feature" or "Adds feature"
- Keep subject line under 50 characters
- Separate subject and body with blank line
- Wrap body at 72 characters
- Reference issues/PRs in body if relevant
- Auto-push immediately after creating commits
- Linting can be skipped for urgent fixes, but not recommended
