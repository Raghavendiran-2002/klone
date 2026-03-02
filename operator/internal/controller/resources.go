package controller

import (
	"fmt"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
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

	// Add node affinity if enabled
	if cluster.Spec.Storage.NodeAffinity != nil && cluster.Spec.Storage.NodeAffinity.Enabled {
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

func int64Ptr(i int64) *int64 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
