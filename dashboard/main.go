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
	"strings"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	k8sClient client.Client
	scheme    = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = klonev1alpha1.AddToScheme(scheme)
}

func main() {
	// Initialize Kubernetes client
	if err := initClient(); err != nil {
		log.Fatalf("Failed to initialize Kubernetes client: %v", err)
	}

	// Serve static files
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/clusters", listClusters)
	http.HandleFunc("/api/clusters/", getCluster)
	http.HandleFunc("/api/terminal/", proxyTerminal)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting dashboard server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func initClient() error {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}
	}

	k8sClient, err = client.New(config, client.Options{Scheme: scheme})
	return err
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	html := getIndexHTML()
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(html)); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func listClusters(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	clusterList := &klonev1alpha1.KloneClusterList{}
	if err := k8sClient.List(ctx, clusterList); err != nil {
		http.Error(w, fmt.Sprintf("Failed to list clusters: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clusterList); err != nil {
		log.Printf("Failed to encode cluster list: %v", err)
	}
}

func getCluster(w http.ResponseWriter, r *http.Request) {
	// Extract cluster name from path
	name := r.URL.Path[len("/api/clusters/"):]
	if name == "" {
		http.Error(w, "Cluster name required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: "default", // Assuming clusters are in default namespace
	}, cluster); err != nil {
		http.Error(w, fmt.Sprintf("Failed to get cluster: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cluster); err != nil {
		log.Printf("Failed to encode cluster: %v", err)
	}
}

func proxyTerminal(w http.ResponseWriter, r *http.Request) {
	// Extract cluster name from path: /api/terminal/{name}/...
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/terminal/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Cluster name required", http.StatusBadRequest)
		return
	}

	clusterName := pathParts[0]
	ctx := context.Background()

	// Get cluster to find namespace
	cluster := &klonev1alpha1.KloneCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      clusterName,
		Namespace: "default",
	}, cluster); err != nil {
		http.Error(w, fmt.Sprintf("Cluster not found: %v", err), http.StatusNotFound)
		return
	}

	namespace := cluster.Status.Namespace
	if namespace == "" {
		namespace = clusterName
	}

	// Forward request to terminal service
	targetURL := fmt.Sprintf("http://klone-terminal.%s.svc.cluster.local", namespace)

	// Create reverse proxy
	target, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Failed to parse target URL", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.Header = r.Header
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/"
	}

	proxy.ServeHTTP(w, r)
}

func getIndexHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Klone Operator Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f5f7fa;
            color: #2d3748;
            padding: 20px;
        }
        .container {
            max-width: 1400px;
            margin: 0 auto;
        }
        header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 10px;
            margin-bottom: 30px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
        }
        h1 { font-size: 28px; margin-bottom: 5px; }
        .subtitle { opacity: 0.9; font-size: 14px; }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .stat-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .stat-value {
            font-size: 32px;
            font-weight: bold;
            color: #667eea;
        }
        .stat-label {
            color: #718096;
            font-size: 14px;
            margin-top: 5px;
        }
        .clusters-grid {
            display: grid;
            gap: 20px;
        }
        .cluster-card {
            background: white;
            border-radius: 8px;
            padding: 24px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .cluster-card:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 16px rgba(0,0,0,0.15);
        }
        .cluster-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 16px;
            padding-bottom: 16px;
            border-bottom: 2px solid #edf2f7;
        }
        .cluster-name {
            font-size: 20px;
            font-weight: 600;
            color: #2d3748;
        }
        .phase-badge {
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 12px;
            font-weight: 600;
            text-transform: uppercase;
        }
        .phase-Creating { background: #fef3c7; color: #92400e; }
        .phase-Running { background: #d1fae5; color: #065f46; }
        .phase-Terminating { background: #fee2e2; color: #991b1b; }
        .phase-Failed { background: #fecaca; color: #7f1d1d; }
        .cluster-info {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 16px;
            margin-bottom: 16px;
        }
        .info-item {
            display: flex;
            flex-direction: column;
        }
        .info-label {
            font-size: 12px;
            color: #718096;
            margin-bottom: 4px;
        }
        .info-value {
            font-size: 14px;
            color: #2d3748;
            font-weight: 500;
        }
        .workloads {
            margin-top: 16px;
            padding-top: 16px;
            border-top: 1px solid #edf2f7;
        }
        .workloads-title {
            font-size: 14px;
            font-weight: 600;
            color: #4a5568;
            margin-bottom: 12px;
        }
        .workload-item {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 8px 12px;
            background: #f7fafc;
            border-radius: 6px;
            margin-bottom: 8px;
        }
        .workload-name {
            font-size: 14px;
            color: #2d3748;
        }
        .workload-status {
            font-size: 13px;
            font-weight: 600;
        }
        .workload-ready { color: #10b981; }
        .workload-not-ready { color: #ef4444; }
        .loading {
            text-align: center;
            padding: 40px;
            color: #718096;
        }
        .spinner {
            border: 3px solid #edf2f7;
            border-top: 3px solid #667eea;
            border-radius: 50%;
            width: 40px;
            height: 40px;
            animation: spin 1s linear infinite;
            margin: 20px auto;
        }
        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
        .empty-state {
            text-align: center;
            padding: 60px 20px;
            background: white;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .empty-state-icon {
            font-size: 64px;
            margin-bottom: 16px;
            opacity: 0.5;
        }
        .empty-state-title {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
        }
        .empty-state-text {
            color: #718096;
        }
        .connect-btn {
            background: #667eea;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 6px;
            font-size: 13px;
            font-weight: 600;
            cursor: pointer;
            transition: background 0.2s;
            margin-top: 16px;
        }
        .connect-btn:hover {
            background: #5568d3;
        }
        .modal {
            display: none;
            position: fixed;
            z-index: 1000;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0,0,0,0.8);
        }
        .modal-content {
            position: relative;
            margin: 2% auto;
            width: 95%;
            height: 90%;
            background: #1e1e1e;
            border-radius: 8px;
            overflow: hidden;
        }
        .close-modal {
            position: absolute;
            top: 10px;
            right: 20px;
            color: white;
            font-size: 32px;
            font-weight: bold;
            cursor: pointer;
            z-index: 1001;
        }
        .close-modal:hover {
            color: #f00;
        }
        .terminal-iframe {
            width: 100%;
            height: 100%;
            border: none;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>Klone Operator Dashboard</h1>
            <div class="subtitle">Kubernetes-in-Kubernetes Cluster Management</div>
        </header>

        <div class="stats">
            <div class="stat-card">
                <div class="stat-value" id="totalClusters">-</div>
                <div class="stat-label">Total Clusters</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="runningClusters">-</div>
                <div class="stat-label">Running</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="creatingClusters">-</div>
                <div class="stat-label">Creating</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="failedClusters">-</div>
                <div class="stat-label">Failed</div>
            </div>
        </div>

        <div id="clustersContainer">
            <div class="loading">
                <div class="spinner"></div>
                <div>Loading clusters...</div>
            </div>
        </div>
    </div>

    <!-- Terminal Modal -->
    <div id="terminalModal" class="modal">
        <div class="modal-content">
            <span class="close-modal" onclick="closeTerminal()">&times;</span>
            <iframe id="terminalIframe" class="terminal-iframe"></iframe>
        </div>
    </div>

    <script>
        async function loadClusters() {
            try {
                const response = await fetch('/api/clusters');
                const data = await response.json();

                const clusters = data.items || [];

                // Update stats
                const stats = {
                    total: clusters.length,
                    running: clusters.filter(c => c.status?.phase === 'Running').length,
                    creating: clusters.filter(c => c.status?.phase === 'Creating').length,
                    failed: clusters.filter(c => c.status?.phase === 'Failed').length
                };

                document.getElementById('totalClusters').textContent = stats.total;
                document.getElementById('runningClusters').textContent = stats.running;
                document.getElementById('creatingClusters').textContent = stats.creating;
                document.getElementById('failedClusters').textContent = stats.failed;

                // Render clusters
                renderClusters(clusters);
            } catch (error) {
                console.error('Failed to load clusters:', error);
                const errorHTML = '<div class="empty-state">' +
                    '<div class="empty-state-icon">⚠️</div>' +
                    '<div class="empty-state-title">Failed to load clusters</div>' +
                    '<div class="empty-state-text">Error: ' + error.message + '</div>' +
                    '</div>';
                document.getElementById('clustersContainer').innerHTML = errorHTML;
            }
        }

        function renderClusters(clusters) {
            const container = document.getElementById('clustersContainer');

            if (clusters.length === 0) {
                const emptyHTML = '<div class="empty-state">' +
                    '<div class="empty-state-icon">📦</div>' +
                    '<div class="empty-state-title">No Clusters Found</div>' +
                    '<div class="empty-state-text">Create your first KloneCluster to get started</div>' +
                    '</div>';
                container.innerHTML = emptyHTML;
                return;
            }

            const html = clusters.map(cluster => {
                const status = cluster.status || {};
                const spec = cluster.spec || {};
                const phase = status.phase || 'Unknown';
                const workloads = status.workloads || [];

                const terminalReady = status.conditions?.find(c => c.type === 'TerminalReady' && c.status === 'True');

                const namespaceHTML = '<div class="info-item">' +
                    '<div class="info-label">Namespace</div>' +
                    '<div class="info-value">' + (status.namespace || '-') + '</div></div>';
                const clusterCIDRHTML = '<div class="info-item">' +
                    '<div class="info-label">Cluster CIDR</div>' +
                    '<div class="info-value">' + (status.clusterCIDR || '-') + '</div></div>';
                const serviceCIDRHTML = '<div class="info-item">' +
                    '<div class="info-label">Service CIDR</div>' +
                    '<div class="info-value">' + (status.serviceCIDR || '-') + '</div></div>';
                const ingressTypeHTML = '<div class="info-item">' +
                    '<div class="info-label">Ingress Type</div>' +
                    '<div class="info-value">' + (spec.ingress?.type || 'none') + '</div></div>';

                const workloadsHTML = workloads.length > 0 ?
                    '<div class="workloads"><div class="workloads-title">Workloads</div>' +
                    workloads.map(w => {
                        const isReady = w.ready === w.desired;
                        const statusClass = isReady ? 'workload-ready' : 'workload-not-ready';
                        return '<div class="workload-item">' +
                            '<div class="workload-name">' + w.kind + '/' + w.name + '</div>' +
                            '<div class="workload-status ' + statusClass + '">' +
                            w.ready + '/' + w.desired + '</div></div>';
                    }).join('') + '</div>' : '';

                const terminalBtn = terminalReady ?
                    '<button class="connect-btn" onclick="connectTerminal(\'' +
                    cluster.metadata.name + '\')">🖥️ Connect Terminal</button>' : '';

                return '<div class="cluster-card">' +
                    '<div class="cluster-header">' +
                    '<div class="cluster-name">' + cluster.metadata.name + '</div>' +
                    '<div class="phase-badge phase-' + phase + '">' + phase + '</div>' +
                    '</div>' +
                    '<div class="cluster-info">' +
                    namespaceHTML + clusterCIDRHTML + serviceCIDRHTML + ingressTypeHTML +
                    '</div>' +
                    workloadsHTML +
                    terminalBtn +
                    '</div>';
            }).join('');

            container.innerHTML = '<div class="clusters-grid">' + html + '</div>';
        }

        // Terminal functions
        function connectTerminal(clusterName) {
            const modal = document.getElementById('terminalModal');
            const iframe = document.getElementById('terminalIframe');
            iframe.src = '/api/terminal/' + clusterName + '/';
            modal.style.display = 'block';
        }

        function closeTerminal() {
            const modal = document.getElementById('terminalModal');
            const iframe = document.getElementById('terminalIframe');
            modal.style.display = 'none';
            iframe.src = '';
        }

        // Close modal on escape key
        document.addEventListener('keydown', function(event) {
            if (event.key === 'Escape') {
                closeTerminal();
            }
        });

        // Initial load
        loadClusters();

        // Reload every 10 seconds
        setInterval(loadClusters, 10000);
    </script>
</body>
</html>`
}
