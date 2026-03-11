package controller

import (
	"fmt"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildNamespace creates a Namespace for the KloneCluster
func BuildNamespace(cluster *klonev1alpha1.KloneCluster) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetNamespaceName(cluster.Name),
			Labels: map[string]string{
				"klone-managed":                "true",
				"klone-cluster-name":           cluster.Name,
				"app.kubernetes.io/name":       "klone",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/managed-by": "klone-operator",
			},
		},
	}
}

// BuildPersistentVolume creates a PersistentVolume for the KloneCluster
func BuildPersistentVolume(cluster *klonev1alpha1.KloneCluster) *corev1.PersistentVolume {
	return BuildPersistentVolumeWithNode(cluster, "")
}

// BuildPersistentVolumeWithNode creates a PersistentVolume for the KloneCluster with optional node targeting
// If targetNode is provided, it will set node affinity to that specific node
func BuildPersistentVolumeWithNode(cluster *klonev1alpha1.KloneCluster, targetNode string) *corev1.PersistentVolume {
	pvName := GetPVName(cluster.Name)
	namespaceName := GetNamespaceName(cluster.Name)

	// Get storage configuration with defaults
	storageClass := cluster.Spec.Storage.StorageClass
	if storageClass == "" {
		storageClass = "local-path"
	}

	size := cluster.Spec.Storage.Size
	if size == "" {
		size = "5Gi"
	}

	hostPath := cluster.Spec.Storage.HostPath
	if hostPath == "" {
		hostPath = "/home/raghav/klone"
	}

	hostPathStr := fmt.Sprintf("%s/%s", hostPath, cluster.Name)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				"klone-managed":      "true",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(size),
			},
			VolumeMode:                    volumeModePtr(corev1.PersistentVolumeFilesystem),
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			StorageClassName:              storageClass,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPathStr,
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Namespace: namespaceName,
				Name:      GetPVCName(),
			},
		},
	}

	// If targetNode is specified, set node affinity to that specific node
	if targetNode != "" {
		pv.Spec.NodeAffinity = &corev1.VolumeNodeAffinity{
			Required: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{targetNode},
							},
						},
					},
				},
			},
		}
	} else if cluster.Spec.Storage.NodeAffinity != nil && cluster.Spec.Storage.NodeAffinity.Enabled {
		// Add node affinity if enabled in spec
		label := cluster.Spec.Storage.NodeAffinity.Label
		if label == "" {
			label = "primary"
		}

		pv.Spec.NodeAffinity = &corev1.VolumeNodeAffinity{
			Required: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "workload",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{label},
							},
						},
					},
				},
			},
		}
	}

	return pv
}

// BuildPersistentVolumeClaim creates a PVC for the KloneCluster
func BuildPersistentVolumeClaim(cluster *klonev1alpha1.KloneCluster) *corev1.PersistentVolumeClaim {
	namespaceName := GetNamespaceName(cluster.Name)
	pvName := GetPVName(cluster.Name)

	storageClass := cluster.Spec.Storage.StorageClass
	if storageClass == "" {
		storageClass = "local-path"
	}

	size := cluster.Spec.Storage.Size
	if size == "" {
		size = "5Gi"
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetPVCName(),
			Namespace: namespaceName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
			StorageClassName: &storageClass,
			VolumeName:       pvName,
		},
	}
}

// BuildControlPlaneService creates a headless service for k3s control plane
func BuildControlPlaneService(cluster *klonev1alpha1.KloneCluster) *corev1.Service {
	namespaceName := GetNamespaceName(cluster.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetControlPlaneServiceName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app": "k3s-control-plane",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service
			Selector: map[string]string{
				"app": "k3s-control-plane",
			},
			Ports: []corev1.ServicePort{
				{
					Name:     "api",
					Port:     6443,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildTerminalService creates a ClusterIP service for the terminal
func BuildTerminalService(cluster *klonev1alpha1.KloneCluster) *corev1.Service {
	namespaceName := GetNamespaceName(cluster.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetTerminalServiceName(),
			Namespace: namespaceName,
			Labels: map[string]string{
				"app": "klone-terminal",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "klone-terminal",
			},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

// Helper functions

func volumeModePtr(mode corev1.PersistentVolumeMode) *corev1.PersistentVolumeMode {
	return &mode
}

func hostPathTypePtr(t corev1.HostPathType) *corev1.HostPathType {
	return &t
}

func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

// BuildArgoCDServiceAccount creates a ServiceAccount for ArgoCD registration jobs
func BuildArgoCDServiceAccount(cluster *klonev1alpha1.KloneCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klone-argocd-registration",
			Namespace: GetNamespaceName(cluster.Name),
			Labels: map[string]string{
				"app":                          "argocd-registration",
				"klone-cluster":                cluster.Name,
				"app.kubernetes.io/name":       "klone",
				"app.kubernetes.io/component":  "argocd-registration",
				"app.kubernetes.io/managed-by": "klone-operator",
			},
		},
	}
}

// BuildArgoCDRole creates a Role for ArgoCD registration jobs
func BuildArgoCDRole(cluster *klonev1alpha1.KloneCluster) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klone-argocd-registration",
			Namespace: GetNamespaceName(cluster.Name),
			Labels: map[string]string{
				"app":                          "argocd-registration",
				"klone-cluster":                cluster.Name,
				"app.kubernetes.io/name":       "klone",
				"app.kubernetes.io/component":  "argocd-registration",
				"app.kubernetes.io/managed-by": "klone-operator",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/log"},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
		},
	}
}

// BuildArgoCDRoleBinding creates a RoleBinding for ArgoCD registration jobs
func BuildArgoCDRoleBinding(cluster *klonev1alpha1.KloneCluster) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klone-argocd-registration",
			Namespace: GetNamespaceName(cluster.Name),
			Labels: map[string]string{
				"app":                          "argocd-registration",
				"klone-cluster":                cluster.Name,
				"app.kubernetes.io/name":       "klone",
				"app.kubernetes.io/component":  "argocd-registration",
				"app.kubernetes.io/managed-by": "klone-operator",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "klone-argocd-registration",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "klone-argocd-registration",
				Namespace: GetNamespaceName(cluster.Name),
			},
		},
	}
}

// BuildCredentialsSecret creates a Secret containing cluster access credentials
func BuildCredentialsSecret(cluster *klonev1alpha1.KloneCluster, username, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetCredentialsSecretName(cluster.Name),
			Namespace: "default",
			Labels: map[string]string{
				"klone-managed":                "true",
				"klone-cluster-name":           cluster.Name,
				"app.kubernetes.io/name":       "klone",
				"app.kubernetes.io/component":  "credentials",
				"app.kubernetes.io/managed-by": "klone-operator",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": username,
			"password": password,
		},
	}
}
