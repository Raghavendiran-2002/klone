#!/bin/bash

# Cleanup Script for Klone Operator
# This script removes all KloneClusters and uninstalls CRDs

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
FORCE=false
KEEP_CLUSTERS=false
SKIP_CONFIRMATION=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --force)
            FORCE=true
            shift
            ;;
        --keep-clusters)
            KEEP_CLUSTERS=true
            shift
            ;;
        --yes|-y)
            SKIP_CONFIRMATION=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Cleanup script for Klone Operator - removes CRDs and clusters"
            echo ""
            echo "Options:"
            echo "  --force            Force delete stuck clusters (skip finalizers)"
            echo "  --keep-clusters    Only uninstall CRDs, keep clusters"
            echo "  --yes, -y          Skip confirmation prompts"
            echo "  --help             Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                 # Interactive cleanup"
            echo "  $0 --yes           # Cleanup without confirmation"
            echo "  $0 --force         # Force delete stuck clusters"
            echo "  $0 --keep-clusters # Only remove CRDs"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Main cleanup steps
print_header "Klone Operator - Cleanup"

# Step 1: Check for existing clusters
print_header "Step 1: Checking for existing clusters"

CLUSTER_COUNT=$(kubectl get kloneclusters --all-namespaces -o name 2>/dev/null | wc -l || echo 0)

if [ "$CLUSTER_COUNT" -gt 0 ]; then
    print_warning "Found $CLUSTER_COUNT KloneCluster(s):"
    echo ""
    kubectl get kloneclusters --all-namespaces -o custom-columns=NAME:.metadata.name,NAMESPACE:.metadata.namespace,PHASE:.status.phase,AGE:.metadata.creationTimestamp 2>/dev/null || true
    echo ""

    if [ "$KEEP_CLUSTERS" = true ]; then
        print_info "Skipping cluster deletion (--keep-clusters flag used)"
    else
        if [ "$SKIP_CONFIRMATION" = false ]; then
            print_warning "All clusters will be deleted. This action cannot be undone!"
            read -p "Do you want to continue? (yes/no) " -r
            echo
            if [[ ! $REPLY =~ ^[Yy]es$ ]]; then
                print_info "Cleanup cancelled"
                exit 0
            fi
        fi

        # Step 2: Delete all clusters
        print_header "Step 2: Deleting all KloneClusters"

        if [ "$FORCE" = true ]; then
            print_warning "Force deleting all clusters (removing finalizers)..."

            # Get all clusters and force delete
            kubectl get kloneclusters --all-namespaces -o json | \
                jq -r '.items[] | "\(.metadata.namespace)/\(.metadata.name)"' | \
                while IFS='/' read -r ns name; do
                    print_info "Force deleting cluster: $name (namespace: $ns)"

                    # Remove finalizers
                    kubectl patch klonecluster "$name" -n "$ns" \
                        -p '{"metadata":{"finalizers":null}}' \
                        --type=merge 2>/dev/null || true

                    # Delete the cluster
                    kubectl delete klonecluster "$name" -n "$ns" \
                        --grace-period=0 --force 2>/dev/null || true
                done
        else
            print_info "Deleting all clusters gracefully..."

            if kubectl delete kloneclusters --all --all-namespaces --timeout=60s; then
                print_success "All clusters deleted successfully"
            else
                print_warning "Some clusters may be stuck. Use --force to force delete"
            fi
        fi

        # Wait a moment for cleanup
        print_info "Waiting for cluster cleanup..."
        sleep 5

        # Check for remaining clusters
        REMAINING=$(kubectl get kloneclusters --all-namespaces -o name 2>/dev/null | wc -l || echo 0)
        if [ "$REMAINING" -gt 0 ]; then
            print_warning "$REMAINING cluster(s) still exist. They may be in terminating state."
            print_info "You may need to manually clean up stuck clusters or use --force"
        else
            print_success "All clusters have been deleted"
        fi
    fi
else
    print_info "No KloneClusters found"
fi

# Step 3: Check for orphaned namespaces
print_header "Step 3: Checking for orphaned namespaces"

# Look for namespaces with klone-managed label
ORPHANED_NS=$(kubectl get namespaces -l klone-managed=true -o name 2>/dev/null | wc -l || echo 0)

if [ "$ORPHANED_NS" -gt 0 ]; then
    print_warning "Found $ORPHANED_NS orphaned namespace(s):"
    kubectl get namespaces -l klone-managed=true -o custom-columns=NAME:.metadata.name,AGE:.metadata.creationTimestamp 2>/dev/null || true
    echo ""

    if [ "$SKIP_CONFIRMATION" = false ]; then
        read -p "Delete orphaned namespaces? (y/n) " -n 1 -r
        echo
    else
        REPLY="y"
    fi

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        print_info "Deleting orphaned namespaces..."
        kubectl delete namespaces -l klone-managed=true --grace-period=30 || true
        print_success "Orphaned namespaces deleted"
    fi
else
    print_info "No orphaned namespaces found"
fi

# Step 4: Check for orphaned PVs
print_header "Step 4: Checking for orphaned PersistentVolumes"

ORPHANED_PVS=$(kubectl get pv -l klone-managed=true -o name 2>/dev/null | wc -l || echo 0)

if [ "$ORPHANED_PVS" -gt 0 ]; then
    print_warning "Found $ORPHANED_PVS orphaned PersistentVolume(s):"
    kubectl get pv -l klone-managed=true -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,CLAIM:.spec.claimRef.name,SIZE:.spec.capacity.storage,AGE:.metadata.creationTimestamp 2>/dev/null || true
    echo ""

    if [ "$SKIP_CONFIRMATION" = false ]; then
        read -p "Delete orphaned PersistentVolumes? (y/n) " -n 1 -r
        echo
    else
        REPLY="y"
    fi

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        print_info "Deleting orphaned PersistentVolumes..."
        kubectl delete pv -l klone-managed=true || true
        print_success "Orphaned PersistentVolumes deleted"
    fi
else
    print_info "No orphaned PersistentVolumes found"
fi

# Step 5: Uninstall CRDs
print_header "Step 5: Uninstalling CRDs"

if [ "$SKIP_CONFIRMATION" = false ]; then
    print_warning "Uninstalling CRDs will remove the KloneCluster resource definition"
    read -p "Continue with CRD removal? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "CRD removal cancelled"
        exit 0
    fi
fi

print_info "Uninstalling Klone CRDs..."

if make uninstall; then
    print_success "CRDs uninstalled successfully"
else
    print_error "Failed to uninstall CRDs"
    exit 1
fi

# Verify CRD removal
print_info "Verifying CRD removal..."
if kubectl get crd kloneclusters.klone.klone.io &> /dev/null; then
    print_warning "KloneCluster CRD still exists (may be in terminating state)"
else
    print_success "KloneCluster CRD has been removed"
fi

# Summary
print_header "Cleanup Complete"

echo ""
print_success "Cleanup operations completed successfully"
echo ""
print_info "Summary:"
echo "  - KloneClusters: $([ "$KEEP_CLUSTERS" = true ] && echo "Kept" || echo "Deleted")"
echo "  - Orphaned resources: Cleaned up"
echo "  - CRDs: Uninstalled"
echo ""

if [ "$KEEP_CLUSTERS" = true ]; then
    print_warning "Note: Clusters were kept but CRDs are removed."
    print_warning "You won't be able to manage clusters until CRDs are reinstalled."
fi

echo ""
