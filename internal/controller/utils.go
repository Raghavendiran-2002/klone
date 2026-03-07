package controller

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
)

const (
	// TerminalDeploymentName is the name of the terminal deployment
	TerminalDeploymentName = "klone-terminal"
	// DefaultK3sToken is the default token for k3s authentication
	DefaultK3sToken = "supersecrettoken123"
)

// AllocateCIDRs generates unique CIDRs for a cluster based on its name
// Uses MD5 hash to ensure deterministic and conflict-free allocation
func AllocateCIDRs(clusterName string) (clusterCIDR, serviceCIDR string) {
	// Generate MD5 hash of cluster name
	hash := md5.Sum([]byte(clusterName))
	hashStr := hex.EncodeToString(hash[:])

	// Take first 2 characters and convert to int, then mod 50
	// This gives us a range of 0-49 for CIDR allocation
	hashInt := int(hashStr[0])<<8 | int(hashStr[1])
	offset := hashInt % 50

	// Cluster CIDR: 10.100-149.0.0/16
	clusterCIDR = fmt.Sprintf("10.%d.0.0/16", 100+offset)

	// Service CIDR: 10.150-199.0.0/16
	serviceCIDR = fmt.Sprintf("10.%d.0.0/16", 150+offset)

	return clusterCIDR, serviceCIDR
}

// GenerateRandomPassword generates a cryptographically secure random password
func GenerateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	password := make([]byte, length)

	for i := range password {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[num.Int64()]
	}

	return string(password), nil
}

// GetCredentialsSecretName returns the credentials secret name for a cluster
func GetCredentialsSecretName(clusterName string) string {
	return fmt.Sprintf("%s-credentials", clusterName)
}

// GetNamespaceName returns the namespace name for a cluster
func GetNamespaceName(clusterName string) string {
	return clusterName
}

// GetPVName returns the PersistentVolume name for a cluster
func GetPVName(clusterName string) string {
	return fmt.Sprintf("%s-klone-pv", clusterName)
}

// GetControlPlaneStatefulSetName returns the control plane StatefulSet name
func GetControlPlaneStatefulSetName() string {
	return "k3s-control-plane"
}

// GetWorkerDeploymentName returns the worker Deployment name
func GetWorkerDeploymentName() string {
	return "k3s-worker"
}

// GetTerminalDeploymentName returns the terminal Deployment name
func GetTerminalDeploymentName() string {
	return TerminalDeploymentName
}

// GetControlPlaneServiceName returns the headless service name for control plane
func GetControlPlaneServiceName() string {
	return "k3s-control-plane"
}

// GetTerminalServiceName returns the service name for terminal
func GetTerminalServiceName() string {
	return "klone-terminal"
}

// GetIngressName returns the Ingress name for terminal access
func GetIngressName() string {
	return "klone-terminal"
}

// GetPVCName returns the PVC name
func GetPVCName() string {
	return "k3s-data"
}

// KloneFinalizer is the finalizer added to KloneCluster resources
const KloneFinalizer = "klone.io/cleanup"

// ClusterPhases
const (
	PhaseCreating    = "Creating"
	PhaseRunning     = "Running"
	PhaseTerminating = "Terminating"
	PhaseFailed      = "Failed"
)

// Condition types
const (
	ConditionReady         = "Ready"
	ConditionTerminalReady = "TerminalReady"
	ConditionIngressReady  = "IngressReady"
)

// IngressTypes
const (
	IngressTypeTailscale    = "tailscale"
	IngressTypeLoadBalancer = "loadbalancer"
	IngressTypeNone         = "none"
)
