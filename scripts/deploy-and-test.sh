#!/bin/bash

# Deploy and Test Script for Klone Operator
# This script installs CRDs and optionally runs a test cluster

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}ℹ ${NC}$1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_header() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    print_error "kubectl not found. Please install kubectl first."
    exit 1
fi

# Check if we're in the project root
if [ ! -f "Makefile" ] || [ ! -d "config/crd" ]; then
    print_error "Please run this script from the project root directory"
    exit 1
fi

# Parse command line arguments
SKIP_TEST=false
CLUSTER_NAME="test-cluster"

while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-test)
            SKIP_TEST=true
            shift
            ;;
        --cluster-name)
            CLUSTER_NAME="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --skip-test        Skip creating test cluster after CRD installation"
            echo "  --cluster-name     Name for test cluster (default: test-cluster)"
            echo "  --help             Show this help message"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Main deployment steps
print_header "Klone Operator - Deploy and Test"

# Step 1: Install CRDs
print_header "Step 1: Installing CRDs"
print_info "Installing Klone CRDs to cluster..."

if make install; then
    print_success "CRDs installed successfully"
else
    print_error "Failed to install CRDs"
    exit 1
fi

# Verify CRD installation
print_info "Verifying CRD installation..."
if kubectl get crd kloneclusters.klone.klone.io &> /dev/null; then
    print_success "KloneCluster CRD is available"
else
    print_error "KloneCluster CRD not found"
    exit 1
fi

# Show CRD details
print_info "CRD Details:"
kubectl get crd kloneclusters.klone.klone.io -o jsonpath='{.spec.versions[*].name}' | tr ' ' '\n' | sed 's/^/  - Version: /'
echo ""

# Step 2: Check for existing clusters
print_header "Step 2: Checking existing clusters"
EXISTING_CLUSTERS=$(kubectl get kloneclusters --all-namespaces -o name 2>/dev/null | wc -l)
if [ "$EXISTING_CLUSTERS" -gt 0 ]; then
    print_info "Found $EXISTING_CLUSTERS existing KloneCluster(s):"
    kubectl get kloneclusters --all-namespaces -o custom-columns=NAME:.metadata.name,NAMESPACE:.metadata.namespace,PHASE:.status.phase,AGE:.metadata.creationTimestamp
else
    print_info "No existing KloneClusters found"
fi
echo ""

# Step 3: Create test cluster (if not skipped)
if [ "$SKIP_TEST" = false ]; then
    print_header "Step 3: Creating test cluster"

    # Check if test cluster already exists
    if kubectl get klonecluster "$CLUSTER_NAME" &> /dev/null; then
        print_warning "Test cluster '$CLUSTER_NAME' already exists"
        read -p "Delete and recreate? (y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            print_info "Deleting existing cluster..."
            kubectl delete klonecluster "$CLUSTER_NAME" --wait=false
            sleep 5
        else
            print_info "Skipping cluster creation"
            SKIP_TEST=true
        fi
    fi

    if [ "$SKIP_TEST" = false ]; then
        print_info "Creating test cluster '$CLUSTER_NAME'..."

        # Create test cluster manifest
        cat > /tmp/klone-test-cluster.yaml <<EOF
apiVersion: klone.klone.io/v1alpha1
kind: KloneCluster
metadata:
  name: $CLUSTER_NAME
spec:
  k3s:
    image: rancher/k3s:v1.35.1-k3s1
    token: supersecrettoken123
    controlPlane:
      replicas: 1
    worker:
      replicas: 1

  storage:
    storageClass: local-path
    size: 2Gi
    hostPath: /tmp/klone

  terminal:
    image: alpine:3.19
    replicas: 1

  ingress:
    type: none
EOF

        # Apply the manifest
        if kubectl apply -f /tmp/klone-test-cluster.yaml; then
            print_success "Test cluster created successfully"

            print_info "Waiting for cluster to be ready (this may take a minute)..."
            sleep 5

            # Show cluster status
            print_info "Cluster status:"
            kubectl get klonecluster "$CLUSTER_NAME" -o wide

            print_info ""
            print_info "To watch cluster progress, run:"
            echo "  kubectl get klonecluster $CLUSTER_NAME -w"
            print_info ""
            print_info "To view cluster resources, run:"
            echo "  kubectl get all -n $CLUSTER_NAME"
            print_info ""
            print_info "To delete the test cluster, run:"
            echo "  kubectl delete klonecluster $CLUSTER_NAME"
        else
            print_error "Failed to create test cluster"
            exit 1
        fi
    fi
else
    print_info "Skipping test cluster creation (--skip-test flag used)"
fi

# Summary
print_header "Deployment Complete"
print_success "CRDs are installed and ready"

if [ "$SKIP_TEST" = false ]; then
    print_success "Test cluster '$CLUSTER_NAME' is being provisioned"
    echo ""
    print_info "Next steps:"
    echo "  1. Monitor cluster: kubectl get klonecluster $CLUSTER_NAME -w"
    echo "  2. Check pods: kubectl get pods -n $CLUSTER_NAME"
    echo "  3. View logs: kubectl logs -n $CLUSTER_NAME -l app=k3s-control-plane"
fi

echo ""
