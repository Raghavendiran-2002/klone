---
name: deploy-dashboard
description: Build and deploy the Klone Dashboard to the cluster
disable-model-invocation: true
---

Build, version-bump, and deploy the Klone Dashboard to the Kubernetes cluster.

## Workflow

1. **Ask about testing**:
   - Prompt: "Run tests before deploying? (yes/no)"
   - If yes: Run `make test`
   - If tests fail: Stop and report errors

2. **Get current version and increment**:
   - Read `config/dashboard/deployment.yaml`
   - Find line 21 with image tag (format: `image: raghavendiran2002/klone-dashboard:v1.0.12`)
   - Extract current version (e.g., "v1.0.12")
   - Increment patch version: v1.0.12 → v1.0.13
   - New version format: `v{MAJOR}.{MINOR}.{PATCH}`

3. **Build dashboard image**:
   - Run: `docker buildx build --platform linux/amd64,linux/arm64 -t raghavendiran2002/klone-dashboard:v{VERSION} -f dashboard/Dockerfile --push .`
   - This builds multi-arch and pushes to Docker Hub
   - If build fails:
     - Wait 5 seconds
     - Retry once with same command
     - If second failure: Stop and report detailed error

4. **Update version file**:
   - Update `config/dashboard/deployment.yaml`:
     - Line 21: `image: raghavendiran2002/klone-dashboard:v{VERSION}`

5. **Deploy dashboard**:
   - Run: `kubectl apply -f config/dashboard/deployment.yaml`
   - If deploy fails:
     - Wait 5 seconds
     - Retry once
     - If second failure: Stop and report error

6. **Verify deployment**:
   - Run: `kubectl get pods -n klone -l app=klone-dashboard`
   - Check pod status (should be Running)
   - Get pod name and show recent logs (last 20 lines)

7. **Report completion**:
   ```
   ✓ Dashboard deployed successfully!

   Version: v{VERSION}
   Image: raghavendiran2002/klone-dashboard:v{VERSION}

   Pod Status:
   {pod status output}

   Recent Logs:
   {last 20 lines of logs}

   Updated Files:
   - config/dashboard/deployment.yaml
   ```

## Error Handling

- **Test failures**: Show failed test output and stop
- **Build failures**: Show Docker error and stop after second attempt
- **Deploy failures**: Show kubectl error and stop after second attempt
- **Version file not found**: Report missing file and stop

## Important Notes

- Always use multi-arch builds (amd64 + arm64)
- Version format uses 'v' prefix: "v1.0.13"
- The buildx command automatically pushes the image with `--push` flag
- Deploy to `klone` namespace
- Dashboard version is independent of operator version
