---
name: deploy-all
description: Build and deploy both Klone Operator and Dashboard
disable-model-invocation: true
---

Build, version-bump, and deploy both the Klone Operator and Dashboard to the Kubernetes cluster.

This skill combines the deploy-operator and deploy-dashboard workflows into a single coordinated deployment.

## Workflow

1. **Ask about testing (once for both)**:
   - Prompt: "Run tests before deploying? (yes/no)"
   - Store answer for both deployments

2. **Deploy Operator**:
   - Execute the complete deploy-operator workflow
   - Use the stored test answer (don't prompt again)
   - If operator deployment fails: Stop and report error
   - Report operator version deployed

3. **Deploy Dashboard**:
   - Execute the complete deploy-dashboard workflow
   - Use the stored test answer (don't prompt again)
   - If dashboard deployment fails: Warn but don't rollback operator
   - Report dashboard version deployed

4. **Verify both components**:
   - Check operator pod: `kubectl get pods -n klone -l control-plane=controller-manager`
   - Check dashboard pod: `kubectl get pods -n klone -l app=klone-dashboard`
   - Show status of both pods

5. **Report completion**:
   ```
   ✓ Full deployment completed!

   Operator:
   - Version: v{OPERATOR_VERSION}
   - Status: {operator pod status}

   Dashboard:
   - Version: v{DASHBOARD_VERSION}
   - Status: {dashboard pod status}

   Updated Files:
   - helm/klone-operator/Chart.yaml
   - helm/klone-operator/values.yaml
   - config/manager/kustomization.yaml
   - config/dashboard/deployment.yaml

   All components are running in the 'klone' namespace.
   ```

## Error Handling

- **Operator deployment fails**: Stop entire workflow, don't proceed to dashboard
- **Dashboard deployment fails**: Report error but consider deployment partially successful
- **Test failures**: Stop entire workflow before any builds

## Important Notes

- This is the recommended workflow for full system updates
- Operator is deployed first (more critical component)
- Dashboard failure doesn't trigger operator rollback
- Both components use independent versioning
- Both build multi-arch images (amd64 + arm64)
