#!/usr/bin/env python3
"""klone: Klone Cluster Manager API"""
import asyncio
import hashlib
import logging
import os
from datetime import datetime, timezone
from pathlib import Path

import httpx
import websockets
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.responses import HTMLResponse, Response
from kubernetes import client, config
from pydantic import BaseModel
from starlette.requests import Request
import uvicorn

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)

TAILSCALE_DOMAIN = os.getenv("TAILSCALE_DOMAIN", "taile3ca5.ts.net")
K3S_TOKEN = "supersecrettoken123"
K3S_IMAGE = "rancher/k3s:v1.28.5-k3s1"
STORAGE_CLASS = "local-storage"
STORAGE_SIZE = "5Gi"
HTML_PATH = Path("/config/index.html")

config.load_incluster_config()
core_v1 = client.CoreV1Api()
apps_v1 = client.AppsV1Api()
networking_v1 = client.NetworkingV1Api()
custom_objects_v1 = client.CustomObjectsApi()

app = FastAPI(title="klone", version="1.0.0", redirect_slashes=False)

_http_client: httpx.AsyncClient | None = None


@app.get("/healthz")
def healthz():
    return {"status": "ok"}


@app.get("/api/metrics/host")
def get_host_metrics():
    """Get CPU and memory metrics for the host Kubernetes cluster."""
    try:
        # Get node metrics
        node_metrics = custom_objects_v1.list_cluster_custom_object(
            group="metrics.k8s.io",
            version="v1beta1",
            plural="nodes"
        )

        nodes = []
        for item in node_metrics.get("items", []):
            name = item["metadata"]["name"]
            usage = item["usage"]

            # Parse CPU (supports n=nanocores, u=microcores, m=millicores)
            cpu_str = usage.get("cpu", "0")
            if cpu_str.endswith("n"):
                cpu_millicores = int(cpu_str[:-1]) / 1000000
            elif cpu_str.endswith("u"):
                cpu_millicores = int(cpu_str[:-1]) / 1000
            elif cpu_str.endswith("m"):
                cpu_millicores = int(cpu_str[:-1])
            else:
                cpu_millicores = int(cpu_str) * 1000

            # Parse memory (in bytes, e.g., "3648Mi" or "3835699200")
            mem_str = usage.get("memory", "0")
            if mem_str.endswith("Ki"):
                mem_mib = int(mem_str[:-2]) / 1024
            elif mem_str.endswith("Mi"):
                mem_mib = int(mem_str[:-2])
            elif mem_str.endswith("Gi"):
                mem_mib = int(mem_str[:-2]) * 1024
            else:
                mem_mib = int(mem_str) / (1024 * 1024)

            # Get node capacity for percentage calculation
            node_info = core_v1.read_node(name)
            capacity = node_info.status.capacity

            # Parse capacity
            cpu_capacity_str = capacity.get("cpu", "1")
            cpu_capacity_millicores = int(cpu_capacity_str) * 1000

            mem_capacity_str = capacity.get("memory", "0Ki")
            if mem_capacity_str.endswith("Ki"):
                mem_capacity_mib = int(mem_capacity_str[:-2]) / 1024
            elif mem_capacity_str.endswith("Mi"):
                mem_capacity_mib = int(mem_capacity_str[:-2])
            elif mem_capacity_str.endswith("Gi"):
                mem_capacity_mib = int(mem_capacity_str[:-2]) * 1024
            else:
                mem_capacity_mib = int(mem_capacity_str) / (1024 * 1024)

            cpu_percent = (cpu_millicores / cpu_capacity_millicores * 100) if cpu_capacity_millicores > 0 else 0
            mem_percent = (mem_mib / mem_capacity_mib * 100) if mem_capacity_mib > 0 else 0

            nodes.append({
                "name": name,
                "cpu": {
                    "millicores": int(cpu_millicores),
                    "capacity_millicores": int(cpu_capacity_millicores),
                    "percent": round(cpu_percent, 1)
                },
                "memory": {
                    "mib": int(mem_mib),
                    "capacity_mib": int(mem_capacity_mib),
                    "percent": round(mem_percent, 1)
                }
            })

        # Get pod metrics for namespace aggregation
        pod_metrics = custom_objects_v1.list_cluster_custom_object(
            group="metrics.k8s.io",
            version="v1beta1",
            plural="pods"
        )

        namespace_usage = {}
        for item in pod_metrics.get("items", []):
            ns = item["metadata"]["namespace"]
            if ns not in namespace_usage:
                namespace_usage[ns] = {"cpu_millicores": 0, "memory_mib": 0, "pod_count": 0}

            for container in item.get("containers", []):
                usage = container.get("usage", {})

                cpu_str = usage.get("cpu", "0")
                if cpu_str.endswith("n"):
                    cpu_mc = int(cpu_str[:-1]) / 1000000
                elif cpu_str.endswith("u"):
                    cpu_mc = int(cpu_str[:-1]) / 1000
                elif cpu_str.endswith("m"):
                    cpu_mc = int(cpu_str[:-1])
                else:
                    cpu_mc = int(cpu_str) * 1000

                mem_str = usage.get("memory", "0")
                if mem_str.endswith("Ki"):
                    mem_mi = int(mem_str[:-2]) / 1024
                elif mem_str.endswith("Mi"):
                    mem_mi = int(mem_str[:-2])
                elif mem_str.endswith("Gi"):
                    mem_mi = int(mem_str[:-2]) * 1024
                else:
                    mem_mi = int(mem_str) / (1024 * 1024)

                namespace_usage[ns]["cpu_millicores"] += cpu_mc
                namespace_usage[ns]["memory_mib"] += mem_mi

            namespace_usage[ns]["pod_count"] += 1

        namespaces = [
            {
                "name": ns,
                "cpu_millicores": int(data["cpu_millicores"]),
                "memory_mib": int(data["memory_mib"]),
                "pod_count": data["pod_count"]
            }
            for ns, data in sorted(namespace_usage.items())
        ]

        return {"nodes": nodes, "namespaces": namespaces}

    except Exception as e:
        logger.error(f"Error fetching host metrics: {e}")
        raise HTTPException(status_code=503, detail=f"Metrics unavailable: {str(e)}")


@app.get("/", response_class=HTMLResponse)
def root():
    html = HTML_PATH.read_text()
    return html.replace("__TAILSCALE_DOMAIN__", TAILSCALE_DOMAIN)


class CreateClusterRequest(BaseModel):
    name: str


@app.get("/api/clusters")
def list_clusters():
    try:
        ns_list = core_v1.list_namespace(label_selector="klone-managed=true")
        clusters = []
        for ns in ns_list.items:
            # Skip namespaces that are already being deleted
            if ns.status.phase == "Terminating":
                continue
            clusters.append({
                "name": ns.metadata.labels.get("klone-cluster-name", ns.metadata.name),
                "namespace": ns.metadata.name,
                "phase": ns.status.phase,
            })
        return {"clusters": clusters}
    except Exception as e:
        logger.error(f"Error listing clusters: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/api/clusters", status_code=202)
async def create_cluster(req: CreateClusterRequest):
    name = req.name.lower().strip()
    if not name or not name.replace("-", "").isalnum():
        raise HTTPException(status_code=400, detail="Invalid cluster name")
    _do_create_cluster(name)
    # Install metrics-server in the background
    asyncio.create_task(_install_metrics_server(name))
    return {"status": "creating", "name": name}


def _do_create_cluster(name: str):
    ns_name = name
    pv_name = f"{name}-klone-pv"
    host_path = f"/home/raghav/klone/{name}"
    tls_san = f"k3s-control-plane.{ns_name}.svc.cluster.local"

    logger.info(f"[{name}] Creating PV {pv_name}")
    pv = client.V1PersistentVolume(
        metadata=client.V1ObjectMeta(
            name=pv_name,
            labels={"klone-managed": "true", "klone-cluster-name": name},
        ),
        spec=client.V1PersistentVolumeSpec(
            capacity={"storage": STORAGE_SIZE},
            volume_mode="Filesystem",
            access_modes=["ReadWriteOnce"],
            persistent_volume_reclaim_policy="Retain",
            storage_class_name=STORAGE_CLASS,
            host_path=client.V1HostPathVolumeSource(
                path=host_path, type="DirectoryOrCreate"
            ),
            claim_ref=client.V1ObjectReference(
                namespace=ns_name,
                name="k3s-data",
            ),
        ),
    )
    _create_or_ignore(lambda: core_v1.create_persistent_volume(pv))

    logger.info(f"[{name}] Creating Namespace")
    ns = client.V1Namespace(
        metadata=client.V1ObjectMeta(
            name=ns_name,
            labels={"klone-managed": "true", "klone-cluster-name": name},
        )
    )
    _create_or_ignore(lambda: core_v1.create_namespace(ns))

    logger.info(f"[{name}] Creating PVC")
    pvc = client.V1PersistentVolumeClaim(
        metadata=client.V1ObjectMeta(name="k3s-data", namespace=ns_name),
        spec=client.V1PersistentVolumeClaimSpec(
            access_modes=["ReadWriteOnce"],
            storage_class_name=STORAGE_CLASS,
            volume_name=pv_name,
            resources=client.V1VolumeResourceRequirements(
                requests={"storage": STORAGE_SIZE}
            ),
        ),
    )
    _create_or_ignore(
        lambda: core_v1.create_namespaced_persistent_volume_claim(ns_name, pvc)
    )

    logger.info(f"[{name}] Creating headless Service (control-plane)")
    svc_cp = client.V1Service(
        metadata=client.V1ObjectMeta(name="k3s-control-plane", namespace=ns_name),
        spec=client.V1ServiceSpec(
            cluster_ip="None",
            selector={"app": "k3s-control-plane"},
            ports=[client.V1ServicePort(port=6443, target_port=6443)],
        ),
    )
    _create_or_ignore(lambda: core_v1.create_namespaced_service(ns_name, svc_cp))

    logger.info(f"[{name}] Creating terminal Service")
    svc_term = client.V1Service(
        metadata=client.V1ObjectMeta(name="klone-terminal", namespace=ns_name),
        spec=client.V1ServiceSpec(
            type="ClusterIP",
            selector={"app": "klone-terminal"},
            ports=[
                client.V1ServicePort(name="terminal", port=80, target_port=7681)
            ],
        ),
    )
    _create_or_ignore(
        lambda: core_v1.create_namespaced_service(ns_name, svc_term)
    )

    logger.info(f"[{name}] Creating StatefulSet (control-plane)")
    # Unique CIDRs per cluster so multiple k3s instances on the same node
    # don't conflict in the host kernel's iptables rules.
    h = int(hashlib.md5(name.encode()).hexdigest()[:2], 16) % 50  # 0-49
    cluster_cidr = f"10.{h + 100}.0.0/16"   # 10.100–149.0.0/16
    service_cidr = f"10.{h + 150}.0.0/16"   # 10.150–199.0.0/16
    sts = client.V1StatefulSet(
        metadata=client.V1ObjectMeta(name="k3s-control-plane", namespace=ns_name),
        spec=client.V1StatefulSetSpec(
            service_name="k3s-control-plane",
            replicas=1,
            selector=client.V1LabelSelector(
                match_labels={"app": "k3s-control-plane"}
            ),
            template=client.V1PodTemplateSpec(
                metadata=client.V1ObjectMeta(
                    labels={"app": "k3s-control-plane"}
                ),
                spec=client.V1PodSpec(
                    init_containers=[
                        client.V1Container(
                            name="clear-etcd",
                            image="busybox:1.36",
                            command=["sh", "-c", "rm -rf /var/lib/rancher/k3s/server/db/etcd"],
                            volume_mounts=[
                                client.V1VolumeMount(
                                    name="k3s-storage",
                                    mount_path="/var/lib/rancher/k3s",
                                )
                            ],
                        )
                    ],
                    containers=[
                        client.V1Container(
                            name="k3s-server",
                            image=K3S_IMAGE,
                            security_context=client.V1SecurityContext(
                                privileged=True
                            ),
                            args=[
                                "server",
                                # --cluster-init enables embedded etcd (HA mode), which costs
                                # ~150-300 MB RAM per cluster. With a single server replica,
                                # etcd is wasted overhead — two simultaneous clusters exhaust
                                # the node and cause 503s. Omit it: k3s defaults to SQLite
                                # (~10 MB), which is fine for a single-server inner cluster.
                                "--token", K3S_TOKEN,
                                "--tls-san", tls_san,
                                "--write-kubeconfig=/var/lib/rancher/k3s/kubeconfig.yaml",
                                "--write-kubeconfig-mode=644",
                                # host-gw uses kernel routes instead of a VXLAN tunnel.
                                # VXLAN (the default) creates a flannel.1 interface in the
                                # HOST network namespace (privileged pod); a second k3s cluster
                                # on the same node fails because flannel.1 already exists →
                                # flannel crashes → k3s server never becomes ready → 503.
                                # host-gw adds per-cluster routes instead — no shared interface.
                                "--flannel-backend=host-gw",
                                # Unique CIDRs so route entries don't overlap between clusters
                                f"--cluster-cidr={cluster_cidr}",
                                f"--service-cidr={service_cidr}",
                                # Not needed inside Klone; reduces startup noise
                                "--disable=traefik",
                                "--disable=servicelb",
                            ],
                            ports=[
                                client.V1ContainerPort(container_port=6443)
                            ],
                            resources=client.V1ResourceRequirements(
                                requests={"cpu": "500m", "memory": "512Mi"},
                                limits={"cpu": "2000m", "memory": "1Gi"},
                            ),
                            volume_mounts=[
                                client.V1VolumeMount(
                                    name="k3s-storage",
                                    mount_path="/var/lib/rancher/k3s",
                                )
                            ],
                        )
                    ],
                    volumes=[
                        client.V1Volume(
                            name="k3s-storage",
                            persistent_volume_claim=client.V1PersistentVolumeClaimVolumeSource(
                                claim_name="k3s-data"
                            ),
                        )
                    ],
                ),
            ),
        ),
    )
    _create_or_ignore(
        lambda: apps_v1.create_namespaced_stateful_set(ns_name, sts)
    )

    logger.info(f"[{name}] Creating worker Deployment")
    worker = client.V1Deployment(
        metadata=client.V1ObjectMeta(name="k3s-worker", namespace=ns_name),
        spec=client.V1DeploymentSpec(
            replicas=2,
            selector=client.V1LabelSelector(
                match_labels={"app": "k3s-worker"}
            ),
            template=client.V1PodTemplateSpec(
                metadata=client.V1ObjectMeta(labels={"app": "k3s-worker"}),
                spec=client.V1PodSpec(
                    containers=[
                        client.V1Container(
                            name="k3s-agent",
                            image=K3S_IMAGE,
                            security_context=client.V1SecurityContext(
                                privileged=True
                            ),
                            args=[
                                "agent",
                                "--server",
                                f"https://k3s-control-plane.{ns_name}.svc.cluster.local:6443",
                                "--token", K3S_TOKEN,
                            ],
                            volume_mounts=[
                                client.V1VolumeMount(
                                    name="k3s-agent-data",
                                    mount_path="/var/lib/rancher/k3s",
                                )
                            ],
                        )
                    ],
                    volumes=[
                        client.V1Volume(
                            name="k3s-agent-data",
                            empty_dir=client.V1EmptyDirVolumeSource(),
                        )
                    ],
                ),
            ),
        ),
    )
    _create_or_ignore(
        lambda: apps_v1.create_namespaced_deployment(ns_name, worker)
    )

    logger.info(f"[{name}] Creating terminal Deployment")
    terminal_script = "\n".join([
        "CACHE=/k3s/term-cache",
        "mkdir -p \"$CACHE\"",
        "",
        "ARCH=$(uname -m)",
        "[ \"$ARCH\" = \"aarch64\" ] && KARCH=arm64 || KARCH=amd64",
        "",
        "# bash — install once, flag in PVC cache so restarts skip the APK download",
        "if [ ! -f \"$CACHE/.bash-installed\" ]; then",
        "  echo 'Installing bash (first run)...'",
        "  apk add --no-cache bash",
        "  touch \"$CACHE/.bash-installed\"",
        "else",
        "  apk add --no-cache --no-network bash 2>/dev/null || apk add --no-cache bash",
        "fi",
        "",
        "# kubectl — download once, reuse from PVC cache on restarts",
        "if [ ! -x \"$CACHE/kubectl\" ]; then",
        "  echo 'Caching kubectl (first run)...'",
        "  wget -qO \"$CACHE/kubectl\" \"https://dl.k8s.io/release/v1.28.5/bin/linux/$KARCH/kubectl\"",
        "  chmod +x \"$CACHE/kubectl\"",
        "fi",
        "ln -sf \"$CACHE/kubectl\" /usr/local/bin/kubectl",
        "",
        "# ttyd — download once, reuse from PVC cache on restarts",
        "if [ ! -x \"$CACHE/ttyd\" ]; then",
        "  echo 'Caching ttyd (first run)...'",
        "  if [ \"$KARCH\" = \"arm64\" ]; then",
        "    TTYD_ARCH=aarch64",
        "  else",
        "    TTYD_ARCH=x86_64",
        "  fi",
        "  TTYD_VERSION=1.7.7",
        "  wget -qO /tmp/ttyd \"https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${TTYD_ARCH}\"",
        "  mv /tmp/ttyd \"$CACHE/ttyd\"",
        "  chmod +x \"$CACHE/ttyd\"",
        "fi",
        "ln -sf \"$CACHE/ttyd\" /usr/local/bin/ttyd",
        "",
        "mkdir -p /root/.kube",
        "",
        "while [ ! -f /k3s/kubeconfig.yaml ]; do",
        "  echo 'Waiting for kubeconfig...'",
        "  sleep 2",
        "done",
        "",
        "cp /k3s/kubeconfig.yaml /root/.kube/config",
        f"sed -i 's/127.0.0.1/k3s-control-plane.{ns_name}.svc.cluster.local/' /root/.kube/config",
        "",
        'echo "alias k=kubectl" >> /root/.bashrc',
        "",
        "exec ttyd -p 7681 -W bash",
    ])
    terminal = client.V1Deployment(
        metadata=client.V1ObjectMeta(name="klone-terminal", namespace=ns_name),
        spec=client.V1DeploymentSpec(
            replicas=1,
            selector=client.V1LabelSelector(
                match_labels={"app": "klone-terminal"}
            ),
            template=client.V1PodTemplateSpec(
                metadata=client.V1ObjectMeta(labels={"app": "klone-terminal"}),
                spec=client.V1PodSpec(
                    containers=[
                        client.V1Container(
                            name="terminal",
                            image="alpine:3.19",
                            command=["/bin/sh", "-c"],
                            args=[terminal_script],
                            ports=[
                                client.V1ContainerPort(container_port=7681)
                            ],
                            readiness_probe=client.V1Probe(
                                tcp_socket=client.V1TCPSocketAction(port=7681),
                                initial_delay_seconds=3,
                                period_seconds=3,
                                timeout_seconds=5,
                                failure_threshold=40,
                            ),
                            volume_mounts=[
                                client.V1VolumeMount(
                                    name="k3s-storage",
                                    mount_path="/k3s",
                                )
                            ],
                        )
                    ],
                    volumes=[
                        client.V1Volume(
                            name="k3s-storage",
                            persistent_volume_claim=client.V1PersistentVolumeClaimVolumeSource(
                                claim_name="k3s-data"
                            ),
                        )
                    ],
                ),
            ),
        ),
    )
    _create_or_ignore(
        lambda: apps_v1.create_namespaced_deployment(ns_name, terminal)
    )

    logger.info(f"[{name}] Creating Ingress ({name}-terminal)")
    ingress_host = f"{name}-terminal"
    ingress = client.V1Ingress(
        metadata=client.V1ObjectMeta(
            name="klone-terminal",
            namespace=ns_name,
            annotations={
                "tailscale.com/tags": "tag:k8s-operator,tag:k8s"
            }
        ),
        spec=client.V1IngressSpec(
            ingress_class_name="tailscale",
            tls=[client.V1IngressTLS(hosts=[ingress_host])],
            rules=[
                client.V1IngressRule(
                    http=client.V1HTTPIngressRuleValue(
                        paths=[
                            client.V1HTTPIngressPath(
                                path="/",
                                path_type="Prefix",
                                backend=client.V1IngressBackend(
                                    service=client.V1IngressServiceBackend(
                                        name="klone-terminal",
                                        port=client.V1ServiceBackendPort(
                                            name="terminal"
                                        ),
                                    )
                                ),
                            )
                        ]
                    ),
                )
            ],
        ),
    )
    _create_or_ignore(
        lambda: networking_v1.create_namespaced_ingress(ns_name, ingress)
    )
    logger.info(f"[{name}] Cluster creation complete")


async def _install_metrics_server(name: str):
    """Install metrics-server in a newly created k3s cluster."""
    from kubernetes.stream import stream
    import time

    ns_name = name
    max_wait = 300  # 5 minutes
    start_time = time.time()

    logger.info(f"[{name}] Waiting for terminal pod to be ready for metrics-server installation")

    # Wait for terminal pod to be ready
    while time.time() - start_time < max_wait:
        try:
            pods = core_v1.list_namespaced_pod(
                ns_name,
                label_selector="app=klone-terminal"
            )
            if pods.items and pods.items[0].status.phase == "Running":
                terminal_pod = pods.items[0].metadata.name

                # Check if kubectl is working in the pod
                try:
                    test_output = stream(
                        core_v1.connect_get_namespaced_pod_exec,
                        terminal_pod,
                        ns_name,
                        command=["kubectl", "get", "nodes"],
                        stderr=False,
                        stdin=False,
                        stdout=True,
                        tty=False,
                    )
                    if "control-plane" in test_output or "Ready" in test_output:
                        logger.info(f"[{name}] Terminal pod ready, installing metrics-server")
                        break
                except Exception as e:
                    logger.debug(f"[{name}] kubectl not ready yet: {e}")
        except Exception as e:
            logger.debug(f"[{name}] Waiting for terminal pod: {e}")

        await asyncio.sleep(5)
    else:
        logger.warning(f"[{name}] Timeout waiting for terminal pod, skipping metrics-server installation")
        return

    # Install metrics-server
    metrics_server_yaml = """apiVersion: v1
kind: ServiceAccount
metadata:
  labels:
    k8s-app: metrics-server
  name: metrics-server
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    k8s-app: metrics-server
  name: system:metrics-server
rules:
- apiGroups: [""]
  resources: ["nodes/metrics"]
  verbs: ["get"]
- apiGroups: [""]
  resources: ["pods", "nodes"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  labels:
    k8s-app: metrics-server
  name: system:metrics-server
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:metrics-server
subjects:
- kind: ServiceAccount
  name: metrics-server
  namespace: kube-system
---
apiVersion: v1
kind: Service
metadata:
  labels:
    k8s-app: metrics-server
  name: metrics-server
  namespace: kube-system
spec:
  ports:
  - name: https
    port: 443
    protocol: TCP
    targetPort: https
  selector:
    k8s-app: metrics-server
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    k8s-app: metrics-server
  name: metrics-server
  namespace: kube-system
spec:
  selector:
    matchLabels:
      k8s-app: metrics-server
  template:
    metadata:
      labels:
        k8s-app: metrics-server
    spec:
      containers:
      - args:
        - --cert-dir=/tmp
        - --secure-port=10250
        - --kubelet-preferred-address-types=InternalIP
        - --kubelet-use-node-status-port
        - --metric-resolution=15s
        - --kubelet-insecure-tls
        image: registry.k8s.io/metrics-server/metrics-server:v0.7.0
        imagePullPolicy: IfNotPresent
        name: metrics-server
        ports:
        - containerPort: 10250
          name: https
          protocol: TCP
        resources:
          requests:
            cpu: 100m
            memory: 200Mi
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 1000
        volumeMounts:
        - mountPath: /tmp
          name: tmp-dir
      serviceAccountName: metrics-server
      volumes:
      - emptyDir: {}
        name: tmp-dir
---
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  labels:
    k8s-app: metrics-server
  name: v1beta1.metrics.k8s.io
spec:
  group: metrics.k8s.io
  groupPriorityMinimum: 100
  insecureSkipTLSVerify: true
  service:
    name: metrics-server
    namespace: kube-system
  version: v1beta1
  versionPriority: 100
"""

    try:
        # Apply metrics-server manifest
        apply_output = stream(
            core_v1.connect_get_namespaced_pod_exec,
            terminal_pod,
            ns_name,
            command=["sh", "-c", f"cat <<'METRICSEOF' | kubectl apply -f -\n{metrics_server_yaml}\nMETRICSEOF"],
            stderr=True,
            stdin=False,
            stdout=True,
            tty=False,
        )
        logger.info(f"[{name}] Metrics-server installation output: {apply_output}")

        # Wait a bit for metrics-server to start
        await asyncio.sleep(10)

        # Verify installation
        verify_output = stream(
            core_v1.connect_get_namespaced_pod_exec,
            terminal_pod,
            ns_name,
            command=["kubectl", "get", "deployment", "metrics-server", "-n", "kube-system"],
            stderr=False,
            stdin=False,
            stdout=True,
            tty=False,
        )
        logger.info(f"[{name}] Metrics-server verification: {verify_output}")

    except Exception as e:
        logger.error(f"[{name}] Failed to install metrics-server: {e}")


def _force_clear_namespace(ns_name: str):
    """Strip finalizers from every resource type the Tailscale operator
    might have put them on, then force-finalize the namespace itself."""
    patch = {"metadata": {"finalizers": []}}

    # Ingresses — tailscale.com/finalizer
    try:
        for r in networking_v1.list_namespaced_ingress(ns_name).items:
            if r.metadata.finalizers:
                networking_v1.patch_namespaced_ingress(r.metadata.name, ns_name, patch)
                logger.info(f"Cleared ingress finalizers: {r.metadata.name}")
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"ingress finalizers: {e}")

    # StatefulSets — Tailscale operator creates ts-* StatefulSets with finalizers
    try:
        for r in apps_v1.list_namespaced_stateful_set(ns_name).items:
            if r.metadata.finalizers:
                apps_v1.patch_namespaced_stateful_set(r.metadata.name, ns_name, patch)
                logger.info(f"Cleared statefulset finalizers: {r.metadata.name}")
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"statefulset finalizers: {e}")

    # Deployments
    try:
        for r in apps_v1.list_namespaced_deployment(ns_name).items:
            if r.metadata.finalizers:
                apps_v1.patch_namespaced_deployment(r.metadata.name, ns_name, patch)
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"deployment finalizers: {e}")

    # Services
    try:
        for r in core_v1.list_namespaced_service(ns_name).items:
            if r.metadata.finalizers:
                core_v1.patch_namespaced_service(r.metadata.name, ns_name, patch)
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"service finalizers: {e}")

    # Secrets — Tailscale stores device state in secrets with finalizers
    try:
        for r in core_v1.list_namespaced_secret(ns_name).items:
            if r.metadata.finalizers:
                core_v1.patch_namespaced_secret(r.metadata.name, ns_name, patch)
                logger.info(f"Cleared secret finalizers: {r.metadata.name}")
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"secret finalizers: {e}")

    # PVCs
    try:
        for r in core_v1.list_namespaced_persistent_volume_claim(ns_name).items:
            if r.metadata.finalizers:
                core_v1.patch_namespaced_persistent_volume_claim(
                    r.metadata.name, ns_name, patch)
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"pvc finalizers: {e}")

    # Namespace-level spec.finalizers — mirrors:
    #   kubectl get namespace <ns> -o json \
    #     | jq '.spec.finalizers=[]' \
    #     | kubectl replace --raw /api/v1/namespaces/<ns>/finalize -f -
    # GET the live object first so resourceVersion is included, avoiding
    # a 409 Conflict that the scratch-built body would produce.
    try:
        live_ns = core_v1.read_namespace(ns_name)
        live_ns.spec.finalizers = []
        core_v1.replace_namespace_finalize(ns_name, live_ns)
        logger.info(f"Force-cleared namespace spec.finalizers for {ns_name}")
    except client.exceptions.ApiException as e:
        if e.status != 404: logger.warning(f"namespace finalize: {e}")


async def _cleanup_terminating_namespaces():
    """Background task: re-clear finalizers on klone-managed Terminating namespaces.

    The Tailscale operator can re-add a finalizer to the Ingress after the
    initial force-clear that runs during DELETE /api/clusters/{name}.  Without
    this loop, those namespaces stay stuck in Terminating indefinitely.
    """
    while True:
        await asyncio.sleep(30)
        try:
            ns_list = core_v1.list_namespace(label_selector="klone-managed=true")
            for ns in ns_list.items:
                if ns.status.phase == "Terminating":
                    ns_name = ns.metadata.name
                    logger.info(f"[cleanup] Re-clearing finalizers on Terminating namespace: {ns_name}")
                    _force_clear_namespace(ns_name)
        except Exception as e:
            logger.warning(f"[cleanup] Error in background cleanup: {e}")


@app.on_event("startup")
async def startup_event():
    global _http_client
    _http_client = httpx.AsyncClient(
        timeout=2.0,
        limits=httpx.Limits(max_keepalive_connections=20, max_connections=50),
    )
    asyncio.create_task(_cleanup_terminating_namespaces())


@app.on_event("shutdown")
async def shutdown_event():
    if _http_client:
        await _http_client.aclose()


def _delete_tailscale_devices(cluster_name: str):
    """Delete Tailscale devices associated with a cluster from the Tailscale network.

    Uses the Tailscale API with OAuth credentials stored in the operator-oauth secret.
    """
    try:
        import base64

        # Get OAuth credentials from secret
        try:
            secret = core_v1.read_namespaced_secret("operator-oauth", "tailscale")
            client_id = base64.b64decode(secret.data["client_id"]).decode("utf-8")
            client_secret = base64.b64decode(secret.data["client_secret"]).decode("utf-8")
        except Exception as e:
            logger.warning(f"[{cluster_name}] Could not read Tailscale OAuth credentials: {e}")
            return

        # Get OAuth token
        try:
            with httpx.Client(timeout=30.0) as http_client:
                token_response = http_client.post(
                    "https://api.tailscale.com/api/v2/oauth/token",
                    auth=(client_id, client_secret),
                    data={"grant_type": "client_credentials"}
                )
                token_response.raise_for_status()
                access_token = token_response.json()["access_token"]
        except Exception as e:
            logger.warning(f"[{cluster_name}] Could not get Tailscale OAuth token: {e}")
            return

        # List all devices (use the devices endpoint which works with OAuth tokens)
        try:
            with httpx.Client(timeout=30.0) as http_client:
                devices_response = http_client.get(
                    "https://api.tailscale.com/api/v2/devices",
                    headers={"Authorization": f"Bearer {access_token}"}
                )
                devices_response.raise_for_status()
                devices = devices_response.json()["devices"]
        except Exception as e:
            logger.warning(f"[{cluster_name}] Could not list Tailscale devices: {e}")
            return

        # Find and delete devices matching the cluster name
        deleted_count = 0
        with httpx.Client(timeout=30.0) as http_client:
            for device in devices:
                hostname = device.get("hostname", "")
                device_id = device.get("id", "")

                # Match devices like "cluster-name-terminal" or "testing-cluster-raghav-terminal"
                if cluster_name in hostname and "terminal" in hostname:
                    try:
                        delete_response = http_client.delete(
                            f"https://api.tailscale.com/api/v2/device/{device_id}",
                            headers={"Authorization": f"Bearer {access_token}"}
                        )
                        delete_response.raise_for_status()
                        logger.info(f"[{cluster_name}] Deleted Tailscale device: {hostname} (ID: {device_id})")
                        deleted_count += 1
                    except Exception as e:
                        logger.warning(f"[{cluster_name}] Could not delete Tailscale device {hostname}: {e}")

        if deleted_count > 0:
            logger.info(f"[{cluster_name}] Deleted {deleted_count} Tailscale device(s)")
        else:
            logger.info(f"[{cluster_name}] No Tailscale devices found to delete")

    except Exception as e:
        logger.error(f"[{cluster_name}] Error in Tailscale device deletion: {e}")


@app.delete("/api/clusters/{name}", status_code=202)
def delete_cluster(name: str):
    ns_name = name
    pv_name = f"{name}-klone-pv"
    try:
        # 1. Clear ALL resource finalizers (ingresses, ts-* StatefulSets, secrets, …)
        _force_clear_namespace(ns_name)

        # 2. Explicitly delete Ingresses in the namespace
        try:
            ingresses = networking_v1.list_namespaced_ingress(ns_name)
            for ingress in ingresses.items:
                # Clear finalizers first
                networking_v1.patch_namespaced_ingress(
                    ingress.metadata.name, ns_name, {"metadata": {"finalizers": []}})
                # Then delete
                networking_v1.delete_namespaced_ingress(
                    ingress.metadata.name, ns_name,
                    body=client.V1DeleteOptions(grace_period_seconds=0))
                logger.info(f"[{name}] Ingress {ingress.metadata.name} deleted")
        except client.exceptions.ApiException as e:
            if e.status != 404:
                logger.warning(f"[{name}] Error deleting Ingresses: {e}")

        # 3. Explicitly delete PVC before namespace deletion
        try:
            pvcs = core_v1.list_namespaced_persistent_volume_claim(ns_name)
            for pvc in pvcs.items:
                # Clear finalizers first
                core_v1.patch_namespaced_persistent_volume_claim(
                    pvc.metadata.name, ns_name, {"metadata": {"finalizers": []}})
                # Then delete
                core_v1.delete_namespaced_persistent_volume_claim(
                    pvc.metadata.name, ns_name)
                logger.info(f"[{name}] PVC {pvc.metadata.name} deleted")
        except client.exceptions.ApiException as e:
            if e.status != 404:
                logger.warning(f"[{name}] Error deleting PVCs: {e}")

        # 4. Delete Tailscale ingress resources in tailscale namespace
        #    Tailscale operator creates StatefulSets with labels identifying the parent ingress
        try:
            # Find Tailscale StatefulSets for this cluster using label selector
            label_selector = (
                f"tailscale.com/parent-resource-ns={ns_name},"
                f"tailscale.com/parent-resource=klone-terminal,"
                f"tailscale.com/parent-resource-type=ingress"
            )
            ts_statefulsets = apps_v1.list_namespaced_stateful_set(
                "tailscale", label_selector=label_selector)

            for sts in ts_statefulsets.items:
                # Clear finalizers first
                try:
                    apps_v1.patch_namespaced_stateful_set(
                        sts.metadata.name, "tailscale", {"metadata": {"finalizers": []}})
                except client.exceptions.ApiException:
                    pass  # Ignore if already no finalizers

                # Delete StatefulSet (this will cascade delete the pod)
                try:
                    apps_v1.delete_namespaced_stateful_set(
                        sts.metadata.name, "tailscale",
                        body=client.V1DeleteOptions(grace_period_seconds=0))
                    logger.info(f"[{name}] Tailscale StatefulSet {sts.metadata.name} deleted")
                except client.exceptions.ApiException as e:
                    if e.status != 404:
                        logger.warning(f"[{name}] Could not delete Tailscale StatefulSet: {e}")

            # Also delete any associated secrets in tailscale namespace
            try:
                secrets = core_v1.list_namespaced_secret(
                    "tailscale", label_selector=label_selector)
                for secret in secrets.items:
                    core_v1.patch_namespaced_secret(
                        secret.metadata.name, "tailscale", {"metadata": {"finalizers": []}})
                    core_v1.delete_namespaced_secret(secret.metadata.name, "tailscale")
                    logger.info(f"[{name}] Tailscale secret {secret.metadata.name} deleted")
            except client.exceptions.ApiException as e:
                if e.status != 404:
                    logger.warning(f"[{name}] Error deleting Tailscale secrets: {e}")

        except client.exceptions.ApiException as e:
            if e.status != 404:
                logger.warning(f"[{name}] Error deleting Tailscale resources: {e}")

        # 4.5. Delete Tailscale devices from Tailscale network via API
        _delete_tailscale_devices(name)

        # 5. Delete namespace — cascades to all remaining namespace-scoped resources
        try:
            core_v1.delete_namespace(ns_name)
            logger.info(f"[{name}] Namespace delete requested")
        except client.exceptions.ApiException as e:
            if e.status != 404:
                raise

        # 6. Second pass: re-run force-clear in case the operator re-added finalizers
        #    between steps above
        _force_clear_namespace(ns_name)

        # 7. Delete PV (cluster-scoped resource)
        try:
            # Clear PV finalizers
            core_v1.patch_persistent_volume(pv_name, {"metadata": {"finalizers": []}})
            core_v1.delete_persistent_volume(pv_name)
            logger.info(f"[{name}] PV {pv_name} deleted")
        except client.exceptions.ApiException as e:
            if e.status != 404:
                logger.warning(f"[{name}] Error deleting PV: {e}")

        return {"status": "deleting", "name": name}
    except Exception as e:
        logger.error(f"[{name}] Error deleting cluster: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/api/clusters/{name}/restart", status_code=202)
def restart_cluster(name: str):
    ns_name = name
    now = datetime.now(timezone.utc).isoformat()
    patch = {
        "spec": {
            "template": {
                "metadata": {
                    "annotations": {
                        "kubectl.kubernetes.io/restartedAt": now
                    }
                }
            }
        }
    }
    try:
        for sts in apps_v1.list_namespaced_stateful_set(ns_name).items:
            apps_v1.patch_namespaced_stateful_set(sts.metadata.name, ns_name, patch)
            logger.info(f"[{name}] Restarted StatefulSet {sts.metadata.name}")
        for dep in apps_v1.list_namespaced_deployment(ns_name).items:
            apps_v1.patch_namespaced_deployment(dep.metadata.name, ns_name, patch)
            logger.info(f"[{name}] Restarted Deployment {dep.metadata.name}")
        return {"status": "restarting", "name": name}
    except Exception as e:
        logger.error(f"[{name}] Error restarting cluster: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/api/clusters/{name}/status")
def cluster_status(name: str):
    ns_name = name
    workloads = []
    all_ready = True
    has_error = False
    try:
        try:
            for sts in apps_v1.list_namespaced_stateful_set(ns_name).items:
                desired = sts.spec.replicas or 1
                ready = sts.status.ready_replicas or 0
                workloads.append({
                    "kind": "StatefulSet",
                    "name": sts.metadata.name,
                    "desired": desired,
                    "ready": ready,
                })
                if ready < desired:
                    all_ready = False
        except client.exceptions.ApiException as e:
            if e.status == 404:
                all_ready = False
            else:
                has_error = True

        try:
            for dep in apps_v1.list_namespaced_deployment(ns_name).items:
                desired = dep.spec.replicas or 1
                ready = dep.status.ready_replicas or 0
                workloads.append({
                    "kind": "Deployment",
                    "name": dep.metadata.name,
                    "desired": desired,
                    "ready": ready,
                })
                if ready < desired:
                    all_ready = False
        except client.exceptions.ApiException as e:
            if e.status == 404:
                all_ready = False
            else:
                has_error = True

        if has_error:
            state = "Error"
        elif all_ready and workloads:
            state = "Running"
        else:
            state = "Creating"

        terminal_ready = any(
            w["kind"] == "Deployment"
            and w["name"] == "klone-terminal"
            and w["ready"] >= 1
            for w in workloads
        )

        return {"name": name, "state": state, "workloads": workloads,
                "terminal_ready": terminal_ready}
    except Exception as e:
        logger.error(f"[{name}] Error getting status: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/api/clusters/{name}/terminal-probe")
async def terminal_probe(name: str):
    """Returns 200 if klone-terminal pod is running, 503 otherwise."""
    try:
        # Check if namespace exists
        try:
            core_v1.read_namespace(name)
        except client.exceptions.ApiException as e:
            if e.status == 404:
                return Response(status_code=503)
            raise

        # Check if terminal pod exists and is running
        pods = core_v1.list_namespaced_pod(
            name,
            label_selector="app=klone-terminal"
        )
        if pods.items:
            for pod in pods.items:
                if pod.status.phase == "Running":
                    # Check if all containers are ready
                    if pod.status.container_statuses:
                        all_ready = all(
                            container.ready
                            for container in pod.status.container_statuses
                        )
                        if all_ready:
                            return Response(status_code=200)
        return Response(status_code=503)
    except Exception as e:
        logger.debug(f"Terminal probe error for {name}: {e}")
        return Response(status_code=503)


@app.get("/api/clusters/{name}/metrics")
def get_cluster_metrics(name: str):
    """Get CPU and memory metrics from the nested k3s cluster."""
    ns_name = name
    try:
        # First, get parent cluster workload pod metrics
        workload_pod_metrics = []
        try:
            # Query the parent cluster's metrics API for pods in this namespace
            from kubernetes import client as k8s_client
            api_client = k8s_client.ApiClient()

            # Fetch metrics for all pods in this namespace from parent cluster
            response = api_client.call_api(
                f'/apis/metrics.k8s.io/v1beta1/namespaces/{ns_name}/pods',
                'GET',
                auth_settings=['BearerToken'],
                response_type='object'
            )

            if response[0] and 'items' in response[0]:
                for pod_metric in response[0]['items']:
                    pod_name = pod_metric['metadata']['name']

                    # Sum up all container metrics for this pod
                    total_cpu_nano = 0
                    total_mem_bytes = 0

                    for container in pod_metric.get('containers', []):
                        try:
                            # Parse CPU (format: "123n" nanocores or "456m" millicores)
                            cpu_str = container['usage']['cpu']
                            if cpu_str.endswith('n'):
                                total_cpu_nano += int(cpu_str[:-1])
                            elif cpu_str.endswith('u'):
                                total_cpu_nano += int(cpu_str[:-1]) * 1000
                            elif cpu_str.endswith('m'):
                                total_cpu_nano += int(cpu_str[:-1]) * 1000000
                            else:
                                total_cpu_nano += int(cpu_str) * 1000000000
                        except (ValueError, KeyError):
                            pass  # Skip unparseable CPU values

                        try:
                            # Parse memory (format: "1024Ki" or "1Mi" or "1Gi")
                            mem_str = container['usage']['memory']
                            if mem_str.endswith('Ki'):
                                total_mem_bytes += int(mem_str[:-2]) * 1024
                            elif mem_str.endswith('Mi'):
                                total_mem_bytes += int(mem_str[:-2]) * 1024 * 1024
                            elif mem_str.endswith('Gi'):
                                total_mem_bytes += int(mem_str[:-2]) * 1024 * 1024 * 1024
                            else:
                                total_mem_bytes += int(mem_str)
                        except (ValueError, KeyError):
                            pass  # Skip unparseable memory values

                    workload_pod_metrics.append({
                        "namespace": ns_name,
                        "name": pod_name,
                        "cpu_millicores": int(total_cpu_nano / 1000000),  # Convert nanocores to millicores
                        "memory_mib": int(total_mem_bytes / (1024 * 1024))  # Convert bytes to MiB
                    })
        except Exception as e:
            logger.warning(f"[{name}] Could not fetch parent cluster pod metrics: {e}")

        # Find the terminal pod
        pods = core_v1.list_namespaced_pod(
            ns_name,
            label_selector="app=klone-terminal"
        )
        if not pods.items:
            raise HTTPException(status_code=404, detail="Terminal pod not found")

        terminal_pod = pods.items[0].metadata.name

        # Execute kubectl top nodes inside the nested cluster
        from kubernetes.stream import stream

        try:
            nodes_output = stream(
                core_v1.connect_get_namespaced_pod_exec,
                terminal_pod,
                ns_name,
                command=["sh", "-c", "kubectl top nodes --no-headers 2>&1"],
                stderr=True,
                stdin=False,
                stdout=True,
                tty=False,
            )
            # Check if error message
            if "Metrics API not available" in nodes_output or "error:" in nodes_output:
                nodes_output = ""
        except Exception as e:
            logger.warning(f"[{name}] Could not fetch node metrics: {e}")
            nodes_output = ""

        # Execute kubectl top pods inside the nested cluster
        try:
            pods_output = stream(
                core_v1.connect_get_namespaced_pod_exec,
                terminal_pod,
                ns_name,
                command=["sh", "-c", "kubectl top pods --all-namespaces --no-headers 2>&1"],
                stderr=True,
                stdin=False,
                stdout=True,
                tty=False,
            )
            # Check if error message
            if "Metrics API not available" in pods_output or "error:" in pods_output:
                pods_output = ""
        except Exception as e:
            logger.warning(f"[{name}] Could not fetch pod metrics: {e}")
            pods_output = ""

        # Parse node metrics
        nodes = []
        for line in nodes_output.strip().split("\n"):
            if not line.strip():
                continue
            parts = line.split()
            logger.debug(f"[{name}] Parsing node metrics line: {parts}")
            if len(parts) >= 5:
                try:
                    node_name = parts[0]
                    cpu_str = parts[1]  # e.g., "518m" or "1200m"
                    cpu_percent = parts[2].rstrip("%")  # e.g., "12%"
                    mem_str = parts[3]  # e.g., "3648Mi"
                    mem_percent = parts[4].rstrip("%")  # e.g., "47%"

                    # Parse CPU
                    if cpu_str.endswith("n"):
                        cpu_millicores = int(cpu_str[:-1]) / 1000000
                    elif cpu_str.endswith("u"):
                        cpu_millicores = int(cpu_str[:-1]) / 1000
                    elif cpu_str.endswith("m"):
                        cpu_millicores = int(cpu_str[:-1])
                    else:
                        cpu_millicores = int(cpu_str) * 1000

                    # Parse memory
                    if mem_str.endswith("Ki"):
                        mem_mib = int(mem_str[:-2]) / 1024
                    elif mem_str.endswith("Mi"):
                        mem_mib = int(mem_str[:-2])
                    elif mem_str.endswith("Gi"):
                        mem_mib = int(mem_str[:-2]) * 1024
                    else:
                        mem_mib = int(mem_str) / (1024 * 1024)

                    nodes.append({
                        "name": node_name,
                        "cpu": {
                            "millicores": int(cpu_millicores),
                            "percent": float(cpu_percent)
                        },
                        "memory": {
                            "mib": int(mem_mib),
                            "percent": float(mem_percent)
                        }
                    })
                except (ValueError, IndexError) as e:
                    logger.warning(f"[{name}] Skipping unparseable node metrics line: {line} - {e}")
                    continue

        # Parse pod metrics
        namespace_usage = {}
        pods = []  # Track individual pod metrics
        for line in pods_output.strip().split("\n"):
            if not line.strip():
                continue
            parts = line.split()
            if len(parts) >= 3:
                try:
                    pod_ns = parts[0]
                    pod_name = parts[1]
                    cpu_str = parts[2]  # e.g., "222m"
                    mem_str = parts[3] if len(parts) > 3 else "0Mi"  # e.g., "251Mi"

                    if pod_ns not in namespace_usage:
                        namespace_usage[pod_ns] = {"cpu_millicores": 0, "memory_mib": 0, "pod_count": 0}

                    # Parse CPU
                    if cpu_str.endswith("n"):
                        cpu_mc = int(cpu_str[:-1]) / 1000000
                    elif cpu_str.endswith("u"):
                        cpu_mc = int(cpu_str[:-1]) / 1000
                    elif cpu_str.endswith("m"):
                        cpu_mc = int(cpu_str[:-1])
                    else:
                        cpu_mc = int(cpu_str) * 1000

                    # Parse memory
                    if mem_str.endswith("Ki"):
                        mem_mi = int(mem_str[:-2]) / 1024
                    elif mem_str.endswith("Mi"):
                        mem_mi = int(mem_str[:-2])
                    elif mem_str.endswith("Gi"):
                        mem_mi = int(mem_str[:-2]) * 1024
                    else:
                        try:
                            mem_mi = int(mem_str) / (1024 * 1024)
                        except ValueError:
                            mem_mi = 0

                    namespace_usage[pod_ns]["cpu_millicores"] += cpu_mc
                    namespace_usage[pod_ns]["memory_mib"] += mem_mi
                    namespace_usage[pod_ns]["pod_count"] += 1

                    # Store individual pod metrics
                    pods.append({
                        "namespace": pod_ns,
                        "name": pod_name,
                        "cpu_millicores": int(cpu_mc),
                        "memory_mib": int(mem_mi)
                    })
                except (ValueError, IndexError) as e:
                    logger.warning(f"[{name}] Skipping unparseable pod metrics line: {line} - {e}")
                    continue

        namespaces = [
            {
                "name": ns,
                "cpu_millicores": int(data["cpu_millicores"]),
                "memory_mib": int(data["memory_mib"]),
                "pod_count": data["pod_count"]
            }
            for ns, data in sorted(namespace_usage.items())
        ]

        # If no metrics available, provide basic cluster info
        if not nodes:
            try:
                node_count_output = stream(
                    core_v1.connect_get_namespaced_pod_exec,
                    terminal_pod,
                    ns_name,
                    command=["sh", "-c", "kubectl get nodes --no-headers | wc -l"],
                    stderr=False,
                    stdin=False,
                    stdout=True,
                    tty=False,
                )
                node_count = int(node_count_output.strip())
                nodes = [{"name": f"Cluster has {node_count} node(s)",
                         "info": "Metrics unavailable - Install metrics-server in the cluster to see detailed metrics"}]
            except Exception:
                pass

        # Return workload pod metrics from parent cluster (for workload inline metrics)
        # and nested cluster metrics (for the metrics section)
        return {
            "nodes": nodes,
            "namespaces": namespaces,
            "pods": workload_pod_metrics,  # Parent cluster workload metrics
            "nested_pods": pods,  # Nested cluster pod metrics (for info)
            "metrics_available": bool(nodes and any("cpu" in n for n in nodes))
        }

    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"[{name}] Error fetching cluster metrics: {e}")
        raise HTTPException(status_code=503, detail=f"Metrics unavailable: {str(e)}")


def _create_or_ignore(fn):
    try:
        fn()
    except client.exceptions.ApiException as e:
        if e.status != 409:
            raise


# --- Internal terminal proxy (bypasses Tailscale, uses cluster DNS) ---

_SKIP_REQ_HEADERS = {"host", "connection", "transfer-encoding", "upgrade", "keep-alive"}
# Also strip content-length: httpx auto-decompresses gzip so the original
# compressed length no longer matches — let Starlette set it from actual body size.
_SKIP_RESP_HEADERS = {"content-encoding", "transfer-encoding", "connection",
                      "x-frame-options", "content-security-policy", "content-length"}

# Shown inside the iframe when terminal is not yet reachable; auto-refreshes every 3 s
_LOADING_HTML = """<!DOCTYPE html>
<html><head><meta charset="UTF-8"><meta http-equiv="refresh" content="3">
<title>Terminal starting…</title>
<style>
  body{background:#111827;color:#9ca3af;font-family:monospace;margin:0;
       display:flex;flex-direction:column;align-items:center;
       justify-content:center;height:100vh;gap:16px;}
  .spin{width:36px;height:36px;border:3px solid #374151;
        border-top-color:#60a5fa;border-radius:50%;
        animation:s .8s linear infinite;}
  @keyframes s{to{transform:rotate(360deg)}}
  p{margin:0;} small{color:#4b5563;}
</style></head>
<body>
  <div class="spin"></div>
  <p>Terminal is starting up&hellip;</p>
  <small>Refreshes automatically every 3 s</small>
</body></html>"""


async def _proxy_http(name: str, path: str, request: Request) -> Response:
    target = f"http://klone-terminal.{name}.svc.cluster.local/{path}"
    if request.url.query:
        target += f"?{request.url.query}"
    headers = {k: v for k, v in request.headers.items()
               if k.lower() not in _SKIP_REQ_HEADERS}
    try:
        resp = await _http_client.request(
            method=request.method,
            url=target,
            headers=headers,
            content=await request.body(),
            follow_redirects=True,
        )
        out_headers = {k: v for k, v in resp.headers.items()
                       if k.lower() not in _SKIP_RESP_HEADERS}
        return Response(content=resp.content, status_code=resp.status_code,
                        headers=out_headers)
    except Exception as e:
        logger.warning(f"[{name}] Terminal not reachable ({path or '/'}): {e}")
        # Return a friendly loading page so the iframe never shows a browser error
        return HTMLResponse(content=_LOADING_HTML, status_code=200)


# Two routes cover every case with redirect_slashes=False:
#   /proxy/{name}           → proxies to /
#   /proxy/{name}/{path}    → proxies to /{path}  (path may be empty for trailing slash)
@app.api_route("/proxy/{name}", methods=["GET", "POST", "HEAD"])
async def proxy_root(name: str, request: Request):
    return await _proxy_http(name, "", request)


@app.api_route("/proxy/{name}/{path:path}", methods=["GET", "POST", "HEAD"])
async def proxy_path(name: str, path: str, request: Request):
    return await _proxy_http(name, path, request)


@app.websocket("/proxy/{name}/ws")
async def proxy_ws(name: str, websocket: WebSocket):
    qs = websocket.url.query
    target = f"ws://klone-terminal.{name}.svc.cluster.local/ws"
    if qs:
        target += f"?{qs}"
    req_protos = [p.strip() for p in
                  websocket.headers.get("sec-websocket-protocol", "").split(",")
                  if p.strip()]
    accept_proto = "tty" if "tty" in req_protos else (req_protos[0] if req_protos else None)
    await websocket.accept(subprotocol=accept_proto)
    upstream_protos = [accept_proto] if accept_proto else []
    try:
        async with websockets.connect(target, subprotocols=upstream_protos) as upstream:
            async def fwd_up():
                try:
                    async for data in websocket.iter_bytes():
                        await upstream.send(data)
                except (WebSocketDisconnect, Exception):
                    pass

            async def fwd_down():
                try:
                    async for msg in upstream:
                        if isinstance(msg, bytes):
                            await websocket.send_bytes(msg)
                        else:
                            await websocket.send_text(msg)
                except Exception:
                    pass

            done, pending = await asyncio.wait(
                [asyncio.ensure_future(fwd_up()),
                 asyncio.ensure_future(fwd_down())],
                return_when=asyncio.FIRST_COMPLETED,
            )
            for t in pending:
                t.cancel()
    except Exception as e:
        logger.error(f"WS proxy error {name}: {e}")
        try:
            await websocket.close(1011)
        except Exception:
            pass


if __name__ == "__main__":
    logger.info("Starting klone on port 8000")
    uvicorn.run(app, host="0.0.0.0", port=8000)
