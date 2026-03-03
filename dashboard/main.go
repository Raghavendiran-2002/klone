package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	k8sClient client.Client
	scheme    = k8sruntime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = klonev1alpha1.AddToScheme(scheme)
}

func main() {
	if err := initClient(); err != nil {
		log.Fatalf("Failed to initialize Kubernetes client: %v", err)
	}

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/clusters", clustersHandler)
	http.HandleFunc("/api/clusters/", clusterHandler)
	http.HandleFunc("/api/terminal/", proxyTerminal)

	http.HandleFunc("/api/metrics/host", hostMetrics)
	http.HandleFunc("/api/metrics/cluster/", clusterMetrics)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Dashboard running on %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

//////////////////////////////////////////////////////
// INIT K8S CLIENT
//////////////////////////////////////////////////////

func initClient() error {
	var config *rest.Config
	var err error

	config, err = rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}
	}

	k8sClient, err = client.New(config, client.Options{Scheme: scheme})
	return err
}

//////////////////////////////////////////////////////
// INDEX
//////////////////////////////////////////////////////

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(getIndexHTML()))
}

//////////////////////////////////////////////////////
// CLUSTERS
//////////////////////////////////////////////////////

func clustersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listClusters(w, r)
	case http.MethodPost:
		createCluster(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func listClusters(w http.ResponseWriter, r *http.Request) {
	clusterList := &klonev1alpha1.KloneClusterList{}
	if err := k8sClient.List(context.Background(), clusterList); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(clusterList)
}

func clusterHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clusters/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Cluster name required", 400)
		return
	}

	namespace := "default"
	name := parts[0]

	// Check for namespace/name format
	if len(parts) >= 2 && parts[1] != "" && parts[1] != "logs" && parts[1] != "scale" {
		namespace = parts[0]
		name = parts[1]
	}

	switch r.Method {
	case http.MethodGet:
		// Check for /logs subpath
		if strings.Contains(path, "/logs") {
			getClusterLogs(w, r, namespace, name)
			return
		}
		getCluster(w, r, namespace, name)
	case http.MethodDelete:
		deleteCluster(w, r, namespace, name)
	case http.MethodPatch:
		if strings.Contains(path, "/scale") {
			scaleCluster(w, r, namespace, name)
		} else {
			http.Error(w, "Use /scale endpoint to scale cluster", 400)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getCluster(w http.ResponseWriter, r *http.Request, namespace, name string) {
	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, cluster); err != nil {
		http.Error(w, "Cluster not found: "+err.Error(), 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cluster)
}

func deleteCluster(w http.ResponseWriter, r *http.Request, namespace, name string) {
	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, cluster); err != nil {
		http.Error(w, "Cluster not found: "+err.Error(), 404)
		return
	}

	if err := k8sClient.Delete(context.Background(), cluster); err != nil {
		http.Error(w, "Failed to delete cluster: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message":   "Cluster deletion initiated",
		"name":      name,
		"namespace": namespace,
	})
}

func scaleCluster(w http.ResponseWriter, r *http.Request, namespace, name string) {
	var req struct {
		WorkerReplicas *int32 `json:"workerReplicas"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), 400)
		return
	}

	if req.WorkerReplicas == nil {
		http.Error(w, "workerReplicas is required", 400)
		return
	}

	if *req.WorkerReplicas < 0 {
		http.Error(w, "workerReplicas must be >= 0", 400)
		return
	}

	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, cluster); err != nil {
		http.Error(w, "Cluster not found: "+err.Error(), 404)
		return
	}

	// Update worker replicas
	if cluster.Spec.K3s.Worker == nil {
		cluster.Spec.K3s.Worker = &klonev1alpha1.WorkerSpec{}
	}
	cluster.Spec.K3s.Worker.Replicas = *req.WorkerReplicas

	if err := k8sClient.Update(context.Background(), cluster); err != nil {
		http.Error(w, "Failed to update cluster: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message":        "Cluster scaled successfully",
		"name":           name,
		"namespace":      namespace,
		"workerReplicas": *req.WorkerReplicas,
	})
}

func getClusterLogs(w http.ResponseWriter, r *http.Request, namespace, name string) {
	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, cluster); err != nil {
		http.Error(w, "Cluster not found: "+err.Error(), 404)
		return
	}

	clusterNs := cluster.Status.Namespace
	if clusterNs == "" {
		clusterNs = name
	}

	// Get query params
	podType := r.URL.Query().Get("type") // control-plane or worker

	// List pods in cluster namespace
	pods := &corev1.PodList{}
	if err := k8sClient.List(context.Background(), pods, client.InNamespace(clusterNs)); err != nil {
		http.Error(w, "Failed to list pods: "+err.Error(), 500)
		return
	}

	var logEntries []map[string]any
	for i := range pods.Items {
		pod := &pods.Items[i]
		// Filter by type if specified
		if podType != "" {
			if podType == "control-plane" && !strings.Contains(pod.Name, "control-plane") {
				continue
			}
			if podType == "worker" && !strings.Contains(pod.Name, "worker") {
				continue
			}
		}

		logEntries = append(logEntries, map[string]any{
			"podName":   pod.Name,
			"podPhase":  string(pod.Status.Phase),
			"ready":     isPodReady(pod),
			"restarts":  getPodRestarts(pod),
			"startTime": pod.Status.StartTime,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"clusterName": name,
		"namespace":   clusterNs,
		"pods":        logEntries,
	})
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func getPodRestarts(pod *corev1.Pod) int32 {
	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}
	return restarts
}

func createCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string                           `json:"name"`
		Namespace         string                           `json:"namespace,omitempty"`
		K3sImage          string                           `json:"k3sImage,omitempty"`
		K3sToken          string                           `json:"k3sToken,omitempty"`
		ControlReplicas   *int32                           `json:"controlReplicas,omitempty"`
		WorkerReplicas    *int32                           `json:"workerReplicas,omitempty"`
		StorageSize       string                           `json:"storageSize,omitempty"`
		StorageClass      string                           `json:"storageClass,omitempty"`
		HostPath          string                           `json:"hostPath,omitempty"`
		IngressType       string                           `json:"ingressType,omitempty"`
		TerminalEnabled   *bool                            `json:"terminalEnabled,omitempty"`
		TerminalImage     string                           `json:"terminalImage,omitempty"`
		TerminalReplicas  *int32                           `json:"terminalReplicas,omitempty"`
		MetricsEnabled    *bool                            `json:"metricsEnabled,omitempty"`
		MetricsImage      string                           `json:"metricsImage,omitempty"`
		ArgoCDEnabled     *bool                            `json:"argoCDEnabled,omitempty"`
		ArgoCDNamespace   string                           `json:"argoCDNamespace,omitempty"`
		ArgoCDClusterName string                           `json:"argoCDClusterName,omitempty"`
		ArgoCDLabels      map[string]string                `json:"argoCDLabels,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), 400)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Name is required", 400)
		return
	}

	// Set defaults
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	if req.K3sImage == "" {
		req.K3sImage = "rancher/k3s:v1.35.1-k3s1"
	}
	if req.K3sToken == "" {
		req.K3sToken = "supersecrettoken123"
	}
	if req.ControlReplicas == nil {
		defaultControl := int32(1)
		req.ControlReplicas = &defaultControl
	}
	if req.WorkerReplicas == nil {
		defaultWorker := int32(2)
		req.WorkerReplicas = &defaultWorker
	}
	if req.StorageSize == "" {
		req.StorageSize = "5Gi"
	}
	if req.StorageClass == "" {
		req.StorageClass = "local-path"
	}
	if req.HostPath == "" {
		req.HostPath = "/home/raghav/klone"
	}
	if req.IngressType == "" {
		req.IngressType = "none"
	}
	if req.TerminalEnabled == nil {
		defaultTerminal := true
		req.TerminalEnabled = &defaultTerminal
	}
	if req.TerminalImage == "" {
		req.TerminalImage = "alpine:3.19"
	}
	if req.TerminalReplicas == nil {
		defaultTerminalReplicas := int32(1)
		req.TerminalReplicas = &defaultTerminalReplicas
	}
	if req.MetricsEnabled == nil {
		defaultMetrics := true
		req.MetricsEnabled = &defaultMetrics
	}
	if req.MetricsImage == "" {
		req.MetricsImage = "registry.k8s.io/metrics-server/metrics-server:v0.7.0"
	}
	if req.ArgoCDNamespace == "" {
		req.ArgoCDNamespace = "argocd"
	}

	// Build KloneCluster spec
	cluster := &klonev1alpha1.KloneCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
		Spec: klonev1alpha1.KloneClusterSpec{
			K3s: klonev1alpha1.K3sSpec{
				Image: req.K3sImage,
				Token: req.K3sToken,
				ControlPlane: &klonev1alpha1.ControlPlaneSpec{
					Replicas: *req.ControlReplicas,
				},
				Worker: &klonev1alpha1.WorkerSpec{
					Replicas: *req.WorkerReplicas,
				},
			},
			Storage: klonev1alpha1.StorageSpec{
				StorageClass: req.StorageClass,
				Size:         req.StorageSize,
				HostPath:     req.HostPath,
			},
			Ingress: &klonev1alpha1.IngressSpec{
				Type: req.IngressType,
			},
		},
	}

	// Add optional terminal spec
	if *req.TerminalEnabled {
		cluster.Spec.Terminal = &klonev1alpha1.TerminalSpec{
			Image:    req.TerminalImage,
			Replicas: *req.TerminalReplicas,
		}
	}

	// Add optional metrics-server spec
	if *req.MetricsEnabled {
		cluster.Spec.MetricsServer = &klonev1alpha1.MetricsServerSpec{
			Enabled: *req.MetricsEnabled,
			Image:   req.MetricsImage,
		}
	}

	// Add optional ArgoCD spec
	if req.ArgoCDEnabled != nil || req.ArgoCDClusterName != "" || len(req.ArgoCDLabels) > 0 {
		cluster.Spec.ArgoCD = &klonev1alpha1.ArgoCDSpec{
			Enabled:     req.ArgoCDEnabled,
			Namespace:   req.ArgoCDNamespace,
			ClusterName: req.ArgoCDClusterName,
			Labels:      req.ArgoCDLabels,
		}
	}

	if err := k8sClient.Create(context.Background(), cluster); err != nil {
		http.Error(w, "Failed to create cluster: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Cluster created successfully",
		"name":    req.Name,
		"namespace": req.Namespace,
	})
}

//////////////////////////////////////////////////////
// HOST METRICS
//////////////////////////////////////////////////////

func hostMetrics(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	data := map[string]interface{}{
		"goroutines": runtime.NumGoroutine(),
		"memoryMB":   mem.Alloc / 1024 / 1024,
		"timestamp":  time.Now(),
	}

	json.NewEncoder(w).Encode(data)
}

//////////////////////////////////////////////////////
// CLUSTER METRICS
//////////////////////////////////////////////////////

func clusterMetrics(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/metrics/cluster/")
	if name == "" {
		http.Error(w, "Cluster required", 400)
		return
	}

	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: "default",
	}, cluster); err != nil {
		http.Error(w, "Cluster not found: "+err.Error(), 404)
		return
	}

	ns := cluster.Status.Namespace
	if ns == "" {
		ns = name
	}

	pods := &corev1.PodList{}
	deployments := &appsv1.DeploymentList{}
	services := &corev1.ServiceList{}
	pvcs := &corev1.PersistentVolumeClaimList{}

	k8sClient.List(context.Background(), pods, client.InNamespace(ns))
	k8sClient.List(context.Background(), deployments, client.InNamespace(ns))
	k8sClient.List(context.Background(), services, client.InNamespace(ns))
	k8sClient.List(context.Background(), pvcs, client.InNamespace(ns))

	// Count pod states
	runningPods := 0
	pendingPods := 0
	failedPods := 0
	succeededPods := 0
	totalRestarts := int32(0)

	for _, p := range pods.Items {
		switch p.Status.Phase {
		case corev1.PodRunning:
			runningPods++
		case corev1.PodPending:
			pendingPods++
		case corev1.PodFailed:
			failedPods++
		case corev1.PodSucceeded:
			succeededPods++
		}
		totalRestarts += getPodRestarts(&p)
	}

	// Count deployment readiness
	readyDeployments := 0
	for _, d := range deployments.Items {
		if d.Status.ReadyReplicas == d.Status.Replicas && d.Status.Replicas > 0 {
			readyDeployments++
		}
	}

	// Calculate PVC usage
	var totalPVCSize int64
	for _, pvc := range pvcs.Items {
		if pvc.Status.Phase == corev1.ClaimBound {
			if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				totalPVCSize += storage.Value()
			}
		}
	}

	// Determine cluster health
	health := "healthy"
	if failedPods > 0 || pendingPods > len(pods.Items)/2 {
		health = "degraded"
	}
	if runningPods == 0 && len(pods.Items) > 0 {
		health = "failed"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"clusterName": name,
		"phase":       cluster.Status.Phase,
		"health":      health,
		"pods": map[string]any{
			"total":     len(pods.Items),
			"running":   runningPods,
			"pending":   pendingPods,
			"failed":    failedPods,
			"succeeded": succeededPods,
		},
		"deployments": map[string]any{
			"total": len(deployments.Items),
			"ready": readyDeployments,
		},
		"services": len(services.Items),
		"storage": map[string]any{
			"pvcs":      len(pvcs.Items),
			"totalSize": totalPVCSize,
		},
		"workloads":     cluster.Status.Workloads,
		"restarts":      totalRestarts,
		"ingressURL":    cluster.Status.IngressURL,
		"metricsServer": cluster.Status.MetricsServerInstalled,
		"argoCD":        cluster.Status.ArgoCDRegistered,
	})
}

//////////////////////////////////////////////////////
// TERMINAL PROXY
//////////////////////////////////////////////////////

func proxyTerminal(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/terminal/"), "/")
	if len(pathParts) < 1 || pathParts[0] == "" {
		http.Error(w, "Cluster name required", 400)
		return
	}

	clusterName := pathParts[0]

	// Try to find cluster in all namespaces
	clusterList := &klonev1alpha1.KloneClusterList{}
	if err := k8sClient.List(context.Background(), clusterList); err != nil {
		http.Error(w, "Failed to list clusters: "+err.Error(), 500)
		return
	}

	var cluster *klonev1alpha1.KloneCluster
	for i := range clusterList.Items {
		if clusterList.Items[i].Name == clusterName {
			cluster = &clusterList.Items[i]
			break
		}
	}

	if cluster == nil {
		http.Error(w, "Cluster not found: "+clusterName, 404)
		return
	}

	// Use status namespace if available, otherwise use cluster name
	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = clusterName
	}

	targetURL := fmt.Sprintf("http://klone-terminal.%s.svc.cluster.local", namespace)

	target, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Invalid target URL: "+err.Error(), 500)
		return
	}

	// Create proxy with path rewriting
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Strip the /api/terminal/{clusterName} prefix from the request path
	originalPath := r.URL.Path
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/terminal/"+clusterName)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	// Set the original path in header for debugging
	r.Header.Set("X-Original-Path", originalPath)

	proxy.ServeHTTP(w, r)
}

//////////////////////////////////////////////////////
// HTML
//////////////////////////////////////////////////////

func getIndexHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Klone Dashboard</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        [x-cloak] { display: none !important; }
        .fade-in { animation: fadeIn 0.3s ease-in; }
        @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
        .status-badge {
            display: inline-flex;
            align-items: center;
            padding: 0.25rem 0.75rem;
            border-radius: 9999px;
            font-size: 0.875rem;
            font-weight: 500;
        }
        .status-running { background-color: #dcfce7; color: #166534; }
        .status-pending { background-color: #fef3c7; color: #92400e; }
        .status-failed { background-color: #fee2e2; color: #991b1b; }
        .status-degraded { background-color: #fed7aa; color: #9a3412; }
        .status-healthy { background-color: #dcfce7; color: #166534; }
    </style>
</head>
<body class="bg-gray-50">
    <div class="flex h-screen">
        <!-- Sidebar -->
        <aside class="w-64 bg-gradient-to-b from-blue-600 to-blue-800 text-white flex flex-col">
            <div class="p-6">
                <h1 class="text-2xl font-bold flex items-center">
                    <svg class="w-8 h-8 mr-2" fill="currentColor" viewBox="0 0 20 20">
                        <path d="M10 2a8 8 0 100 16 8 8 0 000-16zM8 11a1 1 0 112 0v3a1 1 0 11-2 0v-3zm1-6a1 1 0 011 1v1a1 1 0 11-2 0V6a1 1 0 011-1z"/>
                    </svg>
                    Klone
                </h1>
                <p class="text-blue-200 text-sm mt-1">Cluster Dashboard</p>
            </div>

            <nav class="flex-1 px-4 space-y-2">
                <button onclick="showView('dashboard')" class="nav-btn w-full text-left px-4 py-3 rounded-lg hover:bg-blue-700 transition flex items-center space-x-3">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"/>
                    </svg>
                    <span>Dashboard</span>
                </button>
                <button onclick="showView('clusters')" class="nav-btn w-full text-left px-4 py-3 rounded-lg hover:bg-blue-700 transition flex items-center space-x-3">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
                    </svg>
                    <span>Clusters</span>
                </button>
                <button onclick="showView('create')" class="nav-btn w-full text-left px-4 py-3 rounded-lg hover:bg-blue-700 transition flex items-center space-x-3">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
                    </svg>
                    <span>Create Cluster</span>
                </button>
            </nav>

            <div class="p-4 border-t border-blue-700">
                <div class="flex items-center space-x-3">
                    <div class="w-10 h-10 rounded-full bg-blue-500 flex items-center justify-center">
                        <svg class="w-6 h-6" fill="currentColor" viewBox="0 0 20 20">
                            <path fill-rule="evenodd" d="M10 9a3 3 0 100-6 3 3 0 000 6zm-7 9a7 7 0 1114 0H3z" clip-rule="evenodd"/>
                        </svg>
                    </div>
                    <div class="flex-1">
                        <p class="text-sm font-medium">Admin</p>
                        <p class="text-xs text-blue-200">System User</p>
                    </div>
                </div>
            </div>
        </aside>

        <!-- Main Content -->
        <main class="flex-1 overflow-auto">
            <div class="p-8">
                <!-- Dashboard View -->
                <div id="dashboard-view" class="view-container">
                    <div class="mb-8">
                        <h2 class="text-3xl font-bold text-gray-800">Dashboard</h2>
                        <p class="text-gray-600 mt-1">Overview of your Klone clusters</p>
                    </div>

                    <!-- Stats Cards -->
                    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <div class="flex items-center justify-between">
                                <div>
                                    <p class="text-gray-500 text-sm">Total Clusters</p>
                                    <p class="text-3xl font-bold text-gray-800 mt-2" id="stat-total-clusters">0</p>
                                </div>
                                <div class="w-12 h-12 bg-blue-100 rounded-lg flex items-center justify-center">
                                    <svg class="w-6 h-6 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
                                    </svg>
                                </div>
                            </div>
                        </div>

                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <div class="flex items-center justify-between">
                                <div>
                                    <p class="text-gray-500 text-sm">Running Pods</p>
                                    <p class="text-3xl font-bold text-green-600 mt-2" id="stat-running-pods">0</p>
                                </div>
                                <div class="w-12 h-12 bg-green-100 rounded-lg flex items-center justify-center">
                                    <svg class="w-6 h-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>
                                    </svg>
                                </div>
                            </div>
                        </div>

                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <div class="flex items-center justify-between">
                                <div>
                                    <p class="text-gray-500 text-sm">Failed Pods</p>
                                    <p class="text-3xl font-bold text-red-600 mt-2" id="stat-failed-pods">0</p>
                                </div>
                                <div class="w-12 h-12 bg-red-100 rounded-lg flex items-center justify-center">
                                    <svg class="w-6 h-6 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
                                    </svg>
                                </div>
                            </div>
                        </div>

                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <div class="flex items-center justify-between">
                                <div>
                                    <p class="text-gray-500 text-sm">Healthy Clusters</p>
                                    <p class="text-3xl font-bold text-green-600 mt-2" id="stat-healthy-clusters">0</p>
                                </div>
                                <div class="w-12 h-12 bg-green-100 rounded-lg flex items-center justify-center">
                                    <svg class="w-6 h-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
                                    </svg>
                                </div>
                            </div>
                        </div>
                    </div>

                    <!-- Recent Clusters -->
                    <div class="bg-white rounded-xl shadow-sm border border-gray-100">
                        <div class="px-6 py-4 border-b border-gray-100">
                            <h3 class="text-lg font-semibold text-gray-800">Recent Clusters</h3>
                        </div>
                        <div id="dashboard-clusters-list" class="p-6">
                            <div class="flex items-center justify-center py-12">
                                <div class="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Clusters View -->
                <div id="clusters-view" class="view-container hidden">
                    <div class="mb-8 flex items-center justify-between">
                        <div>
                            <h2 class="text-3xl font-bold text-gray-800">Clusters</h2>
                            <p class="text-gray-600 mt-1">Manage your Klone clusters</p>
                        </div>
                        <button onclick="showView('create')" class="bg-blue-600 hover:bg-blue-700 text-white px-6 py-3 rounded-lg flex items-center space-x-2 transition">
                            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
                            </svg>
                            <span>Create New Cluster</span>
                        </button>
                    </div>

                    <div id="clusters-grid" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                        <div class="flex items-center justify-center py-12 col-span-full">
                            <div class="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
                        </div>
                    </div>
                </div>

                <!-- Cluster Detail View -->
                <div id="detail-view" class="view-container hidden">
                    <div class="mb-6">
                        <button onclick="showView('clusters')" class="text-blue-600 hover:text-blue-700 flex items-center space-x-2 mb-4">
                            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/>
                            </svg>
                            <span>Back to Clusters</span>
                        </button>
                        <div class="flex items-center justify-between">
                            <div>
                                <h2 class="text-3xl font-bold text-gray-800" id="detail-cluster-name">Cluster Details</h2>
                                <p class="text-gray-600 mt-1" id="detail-cluster-namespace">default</p>
                            </div>
                            <div class="flex items-center space-x-3">
                                <button onclick="connectToTerminal()" class="bg-green-600 hover:bg-green-700 text-white px-4 py-2 rounded-lg flex items-center space-x-2 transition">
                                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
                                    </svg>
                                    <span>Connect to Terminal</span>
                                </button>
                                <button onclick="showScaleModal()" class="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg transition">
                                    Scale Workers
                                </button>
                                <button onclick="deleteCurrentCluster()" class="bg-red-600 hover:bg-red-700 text-white px-4 py-2 rounded-lg transition">
                                    Delete Cluster
                                </button>
                            </div>
                        </div>
                    </div>

                    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6 mb-6">
                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <h4 class="text-sm font-medium text-gray-500 mb-3">Status</h4>
                            <div id="detail-status" class="status-badge status-pending">Loading...</div>
                        </div>
                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <h4 class="text-sm font-medium text-gray-500 mb-3">Health</h4>
                            <div id="detail-health" class="status-badge status-healthy">Healthy</div>
                        </div>
                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <h4 class="text-sm font-medium text-gray-500 mb-3">Ingress URL</h4>
                            <a id="detail-ingress-url" href="#" target="_blank" class="text-blue-600 hover:underline text-sm">N/A</a>
                        </div>
                    </div>

                    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <h3 class="text-lg font-semibold text-gray-800 mb-4">Pod Metrics</h3>
                            <div id="detail-pod-metrics" class="space-y-3">
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Running</span>
                                    <span class="font-semibold text-green-600" id="detail-pods-running">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Pending</span>
                                    <span class="font-semibold text-yellow-600" id="detail-pods-pending">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Failed</span>
                                    <span class="font-semibold text-red-600" id="detail-pods-failed">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Total Restarts</span>
                                    <span class="font-semibold" id="detail-pods-restarts">0</span>
                                </div>
                            </div>
                        </div>

                        <div class="bg-white rounded-xl shadow-sm p-6 border border-gray-100">
                            <h3 class="text-lg font-semibold text-gray-800 mb-4">Resources</h3>
                            <div class="space-y-3">
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Deployments</span>
                                    <span class="font-semibold" id="detail-deployments">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Services</span>
                                    <span class="font-semibold" id="detail-services">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">PVCs</span>
                                    <span class="font-semibold" id="detail-pvcs">0</span>
                                </div>
                                <div class="flex justify-between">
                                    <span class="text-gray-600">Storage Size</span>
                                    <span class="font-semibold" id="detail-storage">0 GB</span>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div class="bg-white rounded-xl shadow-sm border border-gray-100">
                        <div class="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
                            <h3 class="text-lg font-semibold text-gray-800">Pods</h3>
                            <select onchange="filterPods(this.value)" class="px-3 py-2 border border-gray-300 rounded-lg text-sm">
                                <option value="">All Pods</option>
                                <option value="control-plane">Control Plane</option>
                                <option value="worker">Workers</option>
                            </select>
                        </div>
                        <div id="detail-pods-table" class="overflow-x-auto">
                            <table class="w-full">
                                <thead class="bg-gray-50 border-b border-gray-100">
                                    <tr>
                                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Pod Name</th>
                                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Phase</th>
                                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Ready</th>
                                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Restarts</th>
                                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Age</th>
                                    </tr>
                                </thead>
                                <tbody id="pods-tbody" class="divide-y divide-gray-100">
                                    <tr>
                                        <td colspan="5" class="px-6 py-12 text-center text-gray-500">Loading pods...</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>

                <!-- Create Cluster View -->
                <div id="create-view" class="view-container hidden">
                    <div class="mb-8">
                        <h2 class="text-3xl font-bold text-gray-800">Create New Cluster</h2>
                        <p class="text-gray-600 mt-1">Deploy a new Klone cluster with k3s</p>
                    </div>

                    <div class="bg-white rounded-xl shadow-sm border border-gray-100 p-8 max-w-4xl">
                        <form id="create-cluster-form" onsubmit="createNewCluster(event)">
                            <!-- Basic Information -->
                            <div class="mb-8">
                                <h3 class="text-xl font-semibold text-gray-800 mb-4 flex items-center">
                                    <svg class="w-5 h-5 mr-2 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
                                    </svg>
                                    Basic Information
                                </h3>
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Cluster Name *</label>
                                        <input type="text" name="name" required
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            placeholder="my-cluster">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Namespace</label>
                                        <input type="text" name="namespace"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            placeholder="default" value="default">
                                    </div>
                                </div>
                            </div>

                            <!-- K3s Configuration -->
                            <div class="mb-8">
                                <h3 class="text-xl font-semibold text-gray-800 mb-4 flex items-center">
                                    <svg class="w-5 h-5 mr-2 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>
                                    </svg>
                                    K3s Configuration
                                </h3>
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">K3s Image</label>
                                        <input type="text" name="k3sImage"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="rancher/k3s:v1.35.1-k3s1">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">K3s Token</label>
                                        <input type="text" name="k3sToken"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="supersecrettoken123">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Control Plane Replicas</label>
                                        <input type="number" name="controlReplicas" min="1"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="1">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Worker Replicas</label>
                                        <input type="number" name="workerReplicas" min="0"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="2">
                                    </div>
                                </div>
                            </div>

                            <!-- Storage Configuration -->
                            <div class="mb-8">
                                <h3 class="text-xl font-semibold text-gray-800 mb-4 flex items-center">
                                    <svg class="w-5 h-5 mr-2 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4"/>
                                    </svg>
                                    Storage Configuration
                                </h3>
                                <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Storage Size</label>
                                        <input type="text" name="storageSize"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="5Gi">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Storage Class</label>
                                        <input type="text" name="storageClass"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="local-path">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Host Path</label>
                                        <input type="text" name="hostPath"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="/home/raghav/klone">
                                    </div>
                                </div>
                            </div>

                            <!-- Additional Options -->
                            <div class="mb-8">
                                <h3 class="text-xl font-semibold text-gray-800 mb-4 flex items-center">
                                    <svg class="w-5 h-5 mr-2 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4"/>
                                    </svg>
                                    Additional Options
                                </h3>
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Ingress Type</label>
                                        <select name="ingressType" class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent">
                                            <option value="none">None</option>
                                            <option value="tailscale">Tailscale</option>
                                            <option value="loadbalancer">Load Balancer</option>
                                        </select>
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Terminal Image</label>
                                        <input type="text" name="terminalImage"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="alpine:3.19">
                                    </div>
                                    <div class="flex items-center space-x-2">
                                        <input type="checkbox" name="metricsEnabled" id="metricsEnabled" checked class="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500">
                                        <label for="metricsEnabled" class="text-sm font-medium text-gray-700">Enable Metrics Server</label>
                                    </div>
                                    <div class="flex items-center space-x-2">
                                        <input type="checkbox" name="argoCDEnabled" id="argoCDEnabled" class="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500">
                                        <label for="argoCDEnabled" class="text-sm font-medium text-gray-700">Enable ArgoCD Integration</label>
                                    </div>
                                </div>
                            </div>

                            <!-- ArgoCD Configuration (shown when enabled) -->
                            <div id="argocd-config" class="mb-8 hidden">
                                <h3 class="text-xl font-semibold text-gray-800 mb-4 flex items-center">
                                    <svg class="w-5 h-5 mr-2 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
                                    </svg>
                                    ArgoCD Configuration
                                </h3>
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">ArgoCD Namespace</label>
                                        <input type="text" name="argoCDNamespace"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            value="argocd">
                                    </div>
                                    <div>
                                        <label class="block text-sm font-medium text-gray-700 mb-2">Cluster Name in ArgoCD</label>
                                        <input type="text" name="argoCDClusterName"
                                            class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                                            placeholder="Leave empty to use cluster name">
                                    </div>
                                </div>
                            </div>

                            <!-- Submit Buttons -->
                            <div class="flex items-center justify-end space-x-4 pt-6 border-t border-gray-200">
                                <button type="button" onclick="showView('clusters')" class="px-6 py-3 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition">
                                    Cancel
                                </button>
                                <button type="submit" class="bg-blue-600 hover:bg-blue-700 text-white px-8 py-3 rounded-lg flex items-center space-x-2 transition">
                                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
                                    </svg>
                                    <span>Create Cluster</span>
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
        </main>
    </div>

    <!-- Scale Modal -->
    <div id="scale-modal" class="fixed inset-0 bg-black bg-opacity-50 hidden flex items-center justify-center z-50">
        <div class="bg-white rounded-xl shadow-xl p-8 max-w-md w-full mx-4">
            <h3 class="text-2xl font-bold text-gray-800 mb-4">Scale Workers</h3>
            <form onsubmit="scaleClusterWorkers(event)">
                <div class="mb-6">
                    <label class="block text-sm font-medium text-gray-700 mb-2">Worker Replicas</label>
                    <input type="number" id="scale-replicas" min="0" required
                        class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent">
                    <p class="text-sm text-gray-500 mt-2">Current replicas: <span id="current-replicas">0</span></p>
                </div>
                <div class="flex items-center justify-end space-x-4">
                    <button type="button" onclick="hideScaleModal()" class="px-6 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition">
                        Cancel
                    </button>
                    <button type="submit" class="bg-blue-600 hover:bg-blue-700 text-white px-6 py-2 rounded-lg transition">
                        Scale
                    </button>
                </div>
            </form>
        </div>
    </div>

    <script>
        // Global state
        let clusters = [];
        let currentCluster = null;
        let currentClusterPods = [];
        let refreshInterval = null;

        // Initialize
        document.addEventListener('DOMContentLoaded', function() {
            showView('dashboard');
            loadClusters();
            startAutoRefresh();

            // Toggle ArgoCD config visibility
            document.getElementById('argoCDEnabled').addEventListener('change', function() {
                document.getElementById('argocd-config').classList.toggle('hidden', !this.checked);
            });
        });

        // View Management
        function showView(viewName) {
            document.querySelectorAll('.view-container').forEach(v => v.classList.add('hidden'));
            document.querySelectorAll('.nav-btn').forEach(b => b.classList.remove('bg-blue-700'));

            const viewMap = {
                'dashboard': 'dashboard-view',
                'clusters': 'clusters-view',
                'create': 'create-view',
                'detail': 'detail-view'
            };

            const viewId = viewMap[viewName];
            if (viewId) {
                document.getElementById(viewId).classList.remove('hidden');
                document.getElementById(viewId).classList.add('fade-in');
            }

            const buttons = document.querySelectorAll('.nav-btn');
            if (viewName === 'dashboard') buttons[0]?.classList.add('bg-blue-700');
            if (viewName === 'clusters') buttons[1]?.classList.add('bg-blue-700');
            if (viewName === 'create') buttons[2]?.classList.add('bg-blue-700');

            if (viewName === 'clusters') loadClusters();
            if (viewName === 'dashboard') loadDashboardData();
        }

        // Auto-refresh
        function startAutoRefresh() {
            refreshInterval = setInterval(() => {
                const currentView = getCurrentView();
                if (currentView === 'dashboard') loadDashboardData();
                if (currentView === 'clusters') loadClusters();
                if (currentView === 'detail' && currentCluster) loadClusterDetail(currentCluster.metadata.name, currentCluster.metadata.namespace);
            }, 10000); // Refresh every 10 seconds
        }

        function getCurrentView() {
            if (!document.getElementById('dashboard-view').classList.contains('hidden')) return 'dashboard';
            if (!document.getElementById('clusters-view').classList.contains('hidden')) return 'clusters';
            if (!document.getElementById('create-view').classList.contains('hidden')) return 'create';
            if (!document.getElementById('detail-view').classList.contains('hidden')) return 'detail';
            return null;
        }

        // API Calls
        async function loadClusters() {
            try {
                const response = await fetch('/api/clusters');
                const data = await response.json();
                clusters = data.items || [];
                renderClusters();
            } catch (error) {
                console.error('Error loading clusters:', error);
            }
        }

        async function loadDashboardData() {
            try {
                const response = await fetch('/api/clusters');
                const data = await response.json();
                clusters = data.items || [];

                // Update stats
                document.getElementById('stat-total-clusters').textContent = clusters.length;

                // Calculate aggregate metrics
                let totalRunning = 0, totalFailed = 0, healthyCount = 0;

                for (const cluster of clusters) {
                    try {
                        const metricsRes = await fetch('/api/metrics/cluster/' + cluster.metadata.name);
                        const metrics = await metricsRes.json();
                        totalRunning += metrics.pods?.running || 0;
                        totalFailed += metrics.pods?.failed || 0;
                        if (metrics.health === 'healthy') healthyCount++;
                    } catch (e) {}
                }

                document.getElementById('stat-running-pods').textContent = totalRunning;
                document.getElementById('stat-failed-pods').textContent = totalFailed;
                document.getElementById('stat-healthy-clusters').textContent = healthyCount;

                renderDashboardClusters();
            } catch (error) {
                console.error('Error loading dashboard data:', error);
            }
        }

        async function loadClusterDetail(name, namespace = 'default') {
            try {
                const [clusterRes, metricsRes, logsRes] = await Promise.all([
                    fetch('/api/clusters/' + namespace + '/' + name),
                    fetch('/api/metrics/cluster/' + name),
                    fetch('/api/clusters/' + namespace + '/' + name + '/logs')
                ]);

                currentCluster = await clusterRes.json();
                const metrics = await metricsRes.json();
                const logs = await logsRes.json();
                currentClusterPods = logs.pods || [];

                renderClusterDetail(currentCluster, metrics, logs);
            } catch (error) {
                console.error('Error loading cluster detail:', error);
            }
        }

        async function createNewCluster(event) {
            event.preventDefault();
            const form = event.target;
            const formData = new FormData(form);

            const data = {
                name: formData.get('name'),
                namespace: formData.get('namespace') || 'default',
                k3sImage: formData.get('k3sImage'),
                k3sToken: formData.get('k3sToken'),
                controlReplicas: parseInt(formData.get('controlReplicas')),
                workerReplicas: parseInt(formData.get('workerReplicas')),
                storageSize: formData.get('storageSize'),
                storageClass: formData.get('storageClass'),
                hostPath: formData.get('hostPath'),
                ingressType: formData.get('ingressType'),
                terminalImage: formData.get('terminalImage'),
                metricsEnabled: formData.get('metricsEnabled') === 'on',
                argoCDEnabled: formData.get('argoCDEnabled') === 'on'
            };

            if (data.argoCDEnabled) {
                data.argoCDNamespace = formData.get('argoCDNamespace');
                data.argoCDClusterName = formData.get('argoCDClusterName');
            }

            try {
                const response = await fetch('/api/clusters', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data)
                });

                if (response.ok) {
                    alert('Cluster created successfully!');
                    form.reset();
                    showView('clusters');
                } else {
                    const error = await response.text();
                    alert('Error creating cluster: ' + error);
                }
            } catch (error) {
                alert('Error: ' + error.message);
            }
        }

        async function deleteCurrentCluster() {
            if (!currentCluster) return;

            if (!confirm('Are you sure you want to delete cluster "' + currentCluster.metadata.name + '"?')) return;

            try {
                const response = await fetch('/api/clusters/' + currentCluster.metadata.namespace + '/' + currentCluster.metadata.name, {
                    method: 'DELETE'
                });

                if (response.ok) {
                    alert('Cluster deletion initiated');
                    showView('clusters');
                } else {
                    alert('Error deleting cluster');
                }
            } catch (error) {
                alert('Error: ' + error.message);
            }
        }

        function showScaleModal() {
            if (!currentCluster) return;
            const replicas = currentCluster.spec.k3s.worker?.replicas || 0;
            document.getElementById('current-replicas').textContent = replicas;
            document.getElementById('scale-replicas').value = replicas;
            document.getElementById('scale-modal').classList.remove('hidden');
        }

        function hideScaleModal() {
            document.getElementById('scale-modal').classList.add('hidden');
        }

        async function scaleClusterWorkers(event) {
            event.preventDefault();
            if (!currentCluster) return;

            const replicas = parseInt(document.getElementById('scale-replicas').value);

            try {
                const response = await fetch('/api/clusters/' + currentCluster.metadata.namespace + '/' + currentCluster.metadata.name + '/scale', {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ workerReplicas: replicas })
                });

                if (response.ok) {
                    alert('Cluster scaled successfully');
                    hideScaleModal();
                    loadClusterDetail(currentCluster.metadata.name, currentCluster.metadata.namespace);
                } else {
                    alert('Error scaling cluster');
                }
            } catch (error) {
                alert('Error: ' + error.message);
            }
        }

        function filterPods(type) {
            renderPodsTable(type);
        }

        function connectToTerminal() {
            if (!currentCluster) return;

            const namespace = currentCluster.metadata.namespace;
            const name = currentCluster.metadata.name;
            const terminalURL = '/api/terminal/' + name + '/';

            // Open terminal in new window
            window.open(terminalURL, 'terminal-' + name, 'width=1200,height=800,menubar=no,toolbar=no,location=no,status=no');
        }

        function openTerminalForCluster(clusterName) {
            const terminalURL = '/api/terminal/' + clusterName + '/';
            window.open(terminalURL, 'terminal-' + clusterName, 'width=1200,height=800,menubar=no,toolbar=no,location=no,status=no');
        }

        // Rendering Functions
        function renderClusters() {
            const grid = document.getElementById('clusters-grid');

            if (clusters.length === 0) {
                grid.innerHTML = '<div class="col-span-full text-center py-12"><p class="text-gray-500">No clusters found. Create your first cluster!</p></div>';
                return;
            }

            grid.innerHTML = clusters.map(cluster => {
                const status = cluster.status?.phase || 'Pending';
                const name = cluster.metadata.name;
                const namespace = cluster.metadata.namespace;
                const statusClass = 'status-' + status.toLowerCase();

                return '<div class="bg-white rounded-xl shadow-sm border border-gray-100 p-6 hover:shadow-md transition cursor-pointer" onclick="viewClusterDetail(\'' + name + '\', \'' + namespace + '\')">' +
                    '<div class="flex items-center justify-between mb-4">' +
                    '<h3 class="text-xl font-semibold text-gray-800">' + name + '</h3>' +
                    '<span class="status-badge ' + statusClass + '">' + status + '</span>' +
                    '</div>' +
                    '<div class="space-y-2 text-sm text-gray-600">' +
                    '<p><span class="font-medium">Namespace:</span> ' + namespace + '</p>' +
                    '<p><span class="font-medium">Control Plane:</span> ' + (cluster.spec.k3s.controlPlane?.replicas || 0) + ' replicas</p>' +
                    '<p><span class="font-medium">Workers:</span> ' + (cluster.spec.k3s.worker?.replicas || 0) + ' replicas</p>' +
                    '</div>' +
                    '<div class="mt-4 pt-4 border-t border-gray-100 flex justify-end">' +
                    '<button onclick="event.stopPropagation(); viewClusterDetail(\'' + name + '\', \'' + namespace + '\')" class="text-blue-600 hover:text-blue-700 text-sm font-medium">View Details →</button>' +
                    '</div>' +
                    '</div>';
            }).join('');
        }

        function renderDashboardClusters() {
            const list = document.getElementById('dashboard-clusters-list');

            if (clusters.length === 0) {
                list.innerHTML = '<p class="text-center py-8 text-gray-500">No clusters found</p>';
                return;
            }

            const recentClusters = clusters.slice(0, 5);
            list.innerHTML = '<div class="space-y-3">' +
                recentClusters.map(cluster => {
                    const status = cluster.status?.phase || 'Pending';
                    const statusClass = 'status-' + status.toLowerCase();
                    const name = cluster.metadata.name;
                    const isRunning = status === 'Running';
                    return '<div class="flex items-center justify-between p-4 border border-gray-100 rounded-lg hover:bg-gray-50 transition">' +
                        '<div>' +
                        '<p class="font-semibold text-gray-800">' + name + '</p>' +
                        '<p class="text-sm text-gray-600">Namespace: ' + cluster.metadata.namespace + '</p>' +
                        '</div>' +
                        '<div class="flex items-center space-x-3">' +
                        '<span class="status-badge ' + statusClass + '">' + status + '</span>' +
                        (isRunning ? '<button onclick="openTerminalForCluster(\'' + name + '\')" class="bg-green-600 hover:bg-green-700 text-white px-3 py-1 rounded text-sm flex items-center space-x-1 transition">' +
                        '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">' +
                        '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>' +
                        '</svg>' +
                        '<span>Connect</span>' +
                        '</button>' : '') +
                        '</div>' +
                        '</div>';
                }).join('') +
                '</div>';
        }

        function renderClusterDetail(cluster, metrics, logs) {
            document.getElementById('detail-cluster-name').textContent = cluster.metadata.name;
            document.getElementById('detail-cluster-namespace').textContent = 'Namespace: ' + cluster.metadata.namespace;

            const status = cluster.status?.phase || 'Pending';
            const statusClass = 'status-' + status.toLowerCase();
            document.getElementById('detail-status').className = 'status-badge ' + statusClass;
            document.getElementById('detail-status').textContent = status;

            const health = metrics.health || 'unknown';
            const healthClass = 'status-' + health;
            document.getElementById('detail-health').className = 'status-badge ' + healthClass;
            document.getElementById('detail-health').textContent = health.charAt(0).toUpperCase() + health.slice(1);

            const ingressURL = cluster.status?.ingressURL || 'N/A';
            const ingressEl = document.getElementById('detail-ingress-url');
            if (ingressURL !== 'N/A') {
                ingressEl.href = ingressURL;
                ingressEl.textContent = ingressURL;
            } else {
                ingressEl.removeAttribute('href');
                ingressEl.textContent = 'N/A';
            }

            document.getElementById('detail-pods-running').textContent = metrics.pods?.running || 0;
            document.getElementById('detail-pods-pending').textContent = metrics.pods?.pending || 0;
            document.getElementById('detail-pods-failed').textContent = metrics.pods?.failed || 0;
            document.getElementById('detail-pods-restarts').textContent = metrics.restarts || 0;

            document.getElementById('detail-deployments').textContent = metrics.deployments?.total || 0;
            document.getElementById('detail-services').textContent = metrics.services || 0;
            document.getElementById('detail-pvcs').textContent = metrics.storage?.pvcs || 0;
            document.getElementById('detail-storage').textContent = ((metrics.storage?.totalSize || 0) / (1024*1024*1024)).toFixed(2) + ' GB';

            renderPodsTable();
        }

        function renderPodsTable(filterType = '') {
            const tbody = document.getElementById('pods-tbody');
            const pods = filterType ? currentClusterPods.filter(p => p.podName.includes(filterType)) : currentClusterPods;

            if (pods.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="px-6 py-8 text-center text-gray-500">No pods found</td></tr>';
                return;
            }

            tbody.innerHTML = pods.map(pod => {
                const phaseClass = pod.podPhase === 'Running' ? 'text-green-600' : pod.podPhase === 'Failed' ? 'text-red-600' : 'text-yellow-600';
                const readyBadge = pod.ready ? '<span class="text-green-600">✓</span>' : '<span class="text-red-600">✗</span>';
                const age = pod.startTime ? getAge(pod.startTime) : 'N/A';

                return '<tr class="hover:bg-gray-50">' +
                    '<td class="px-6 py-4 text-sm text-gray-800">' + pod.podName + '</td>' +
                    '<td class="px-6 py-4 text-sm ' + phaseClass + '">' + pod.podPhase + '</td>' +
                    '<td class="px-6 py-4 text-sm">' + readyBadge + '</td>' +
                    '<td class="px-6 py-4 text-sm text-gray-600">' + pod.restarts + '</td>' +
                    '<td class="px-6 py-4 text-sm text-gray-600">' + age + '</td>' +
                    '</tr>';
            }).join('');
        }

        function viewClusterDetail(name, namespace) {
            loadClusterDetail(name, namespace);
            showView('detail');
        }

        function getAge(startTime) {
            const start = new Date(startTime);
            const now = new Date();
            const diff = Math.floor((now - start) / 1000);

            if (diff < 60) return diff + 's';
            if (diff < 3600) return Math.floor(diff / 60) + 'm';
            if (diff < 86400) return Math.floor(diff / 3600) + 'h';
            return Math.floor(diff / 86400) + 'd';
        }
    </script>
</body>
</html>`
}
