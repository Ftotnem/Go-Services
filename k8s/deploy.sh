#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

# Color output for better readability
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if cluster supports IPv6
check_ipv6_support() {
    log_info "Checking IPv6 support in cluster..."
    
    # Check if nodes have IPv6 addresses
    IPV6_NODES=$(kubectl get nodes -o jsonpath='{.items[*].status.addresses[?(@.type=="InternalIP")].address}' | grep -c ":" || echo "0")
    
    if [ "$IPV6_NODES" -gt 0 ]; then
        log_success "IPv6 support detected in cluster"
        return 0
    else
        log_warning "No IPv6 addresses detected on nodes. Continuing with IPv4..."
        return 1
    fi
}

# Function to create IPv6 Redis values
create_redis_ipv6_values() {
    log_info "Creating Redis IPv6 configuration..."
    cat <<EOF > redis-ipv6-values.yaml
redis:
  configmap: |
    bind 0.0.0.0 ::
    port 6379
    protected-mode yes
    cluster-enabled yes
    cluster-config-file nodes.conf
    cluster-node-timeout 5000
    appendonly yes
  extraEnvVars:
  - name: REDIS_CLUSTER_ANNOUNCE_HOSTNAME
    value: "true"
auth:
  enabled: true
networkPolicy:
  enabled: false
service:
  type: ClusterIP
headlessService:
  type: ClusterIP
persistence:
  enabled: true
  size: 8Gi
  storageClass: local-path
global:
  storageClass: local-path
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 256Mi
EOF
    log_success "Generated redis-ipv6-values.yaml"
}

# Function to create standard Redis values (IPv4)
create_redis_standard_values() {
    log_info "Creating Redis standard (IPv4) configuration..."
    cat <<EOF > redis-standard-values.yaml
redis:
  configmap: |
    bind 0.0.0.0
    port 6379
    protected-mode yes
    cluster-enabled yes
    cluster-config-file nodes.conf
    cluster-node-timeout 5000
    appendonly yes
  extraEnvVars:
  - name: REDIS_CLUSTER_ANNOUNCE_HOSTNAME
    value: "true"
auth:
  enabled: true
networkPolicy:
  enabled: false
service:
  type: ClusterIP
headlessService:
  type: ClusterIP
persistence:
  enabled: true
  size: 8Gi
  storageClass: local-path
global:
  storageClass: local-path
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 256Mi
EOF
    log_success "Generated redis-standard-values.yaml"
}


# Function to deploy Redis Cluster
deploy_redis_cluster() {
    log_info "Deploying Redis Cluster..."
    
    # Add Bitnami Helm repository
    helm repo add bitnami https://charts.bitnami.com/bitnami --force-update &> /dev/null
    helm repo update &> /dev/null
    log_success "Bitnami Helm repository added and updated."

    local REDIS_PASSWORD_ARG=""
    local EXISTING_REDIS_PASSWORD=""

    # Check if redis-cluster secret exists and retrieve password for upgrades
    if kubectl get secret --namespace "${NAMESPACE}" redis-cluster &> /dev/null; then
        log_info "Redis secret already exists, attempting to retrieve password for upgrade."
        EXISTING_REDIS_PASSWORD=$(kubectl get secret --namespace "${NAMESPACE}" redis-cluster -o jsonpath="{.data.redis-password}" | base64 -d)
        if [ -n "$EXISTING_REDIS_PASSWORD" ]; then
            REDIS_PASSWORD_ARG="--set password=${EXISTING_REDIS_PASSWORD}"
            log_success "Existing Redis password retrieved."
        else
            log_warning "Could not retrieve existing Redis password. Helm upgrade might fail if 'auth.enabled' is true."
        fi
    else
        log_info "Redis secret does not exist, a new password will be generated."
    fi

    # Force standard IPv4 configuration for testing
    create_redis_standard_values # This creates ./redis-standard-values.yaml
    VALUES_FILE="./redis-standard-values.yaml"
    log_info "Forcing standard Redis configuration (IPv4) for testing."

    # Deploy or upgrade the Redis cluster using Helm
    log_info "Running helm upgrade --install redis-cluster bitnami/redis-cluster -n ${NAMESPACE} -f ${VALUES_FILE} ${REDIS_PASSWORD_ARG} --timeout 10m"
    helm upgrade --install redis-cluster bitnami/redis-cluster -n "${NAMESPACE}" -f "${VALUES_FILE}" ${REDIS_PASSWORD_ARG} --timeout 10m
    log_success "Redis Cluster deployment initiated."
    
    # Clean up temporary files
    if [ -f "redis-ipv6-values.yaml" ]; then
        rm redis-ipv6-values.yaml
    fi
    if [ -f "redis-standard-values.yaml" ]; then
        rm redis-standard-values.yaml
    fi
}

# Function to wait for deployment
wait_for_deployment() {
    local DEPLOYMENT_NAME=$1
    local NAMESPACE=$2
    local TIMEOUT=${3:-300} # Default timeout to 300 seconds (5 minutes)
    local START_TIME=$(date +%s)

    log_info "Waiting for pods in deployment/daemonset '${DEPLOYMENT_NAME}' in namespace '${NAMESPACE}' to be ready (timeout: ${TIMEOUT}s)..."

    while true; do
        CURRENT_TIME=$(date +%s)
        ELAPSED_TIME=$((CURRENT_TIME - START_TIME))

        if [ "$ELAPSED_TIME" -ge "$TIMEOUT" ]; then
            log_error "Timeout: Pods for '${DEPLOYMENT_NAME}' did not become ready within ${TIMEOUT} seconds."
            kubectl get pods -l app=${DEPLOYMENT_NAME} -n ${NAMESPACE}
            return 1
        fi

        # Adjust label selector for Redis-cluster if it's not simply 'app=redis-cluster'
        # For Bitnami Redis cluster, label is typically 'app.kubernetes.io/name=redis-cluster'
        # For other deployments, stick to 'app=' or customize.
        local LABEL_SELECTOR=""
        if [[ "$DEPLOYMENT_NAME" == "redis-cluster" ]]; then
            LABEL_SELECTOR="app.kubernetes.io/name=redis-cluster"
        else
            LABEL_SELECTOR="app=${DEPLOYMENT_NAME}"
        fi

        READY_PODS=$(kubectl get pods -l "${LABEL_SELECTOR}" -n "${NAMESPACE}" -o json | jq -r '.items[] | select(.status.phase == "Running" and .status.containerStatuses[]?.ready == true) | .metadata.name' | wc -l)
        TOTAL_PODS=$(kubectl get pods -l "${LABEL_SELECTOR}" -n "${NAMESPACE}" -o json | jq -r '.items | length')

        if [ "$TOTAL_PODS" -gt 0 ] && [ "$READY_PODS" -eq "$TOTAL_PODS" ]; then
            log_success "All ${READY_PODS} pods for '${DEPLOYMENT_NAME}' are ready!"
            return 0
        elif [ "$TOTAL_PODS" -eq 0 ]; then
            log_warning "No pods found for deployment '${DEPLOYMENT_NAME}' yet. Retrying..."
        else
            log_info "Waiting for pods in '${DEPLOYMENT_NAME}' to be ready... (${READY_PODS}/${TOTAL_PODS} ready) - Elapsed: ${ELAPSED_TIME}s"
        fi
        sleep 5
    done
}


# Function to verify deployment status
verify_deployment() {
    log_info "Verifying deployment status..."
    echo ""
    log_info "--- Pod Status ---"
    kubectl get pods -n minecraft-cluster

    echo ""
    log_info "--- Service Status ---"
    kubectl get svc -n minecraft-cluster

    echo ""
    log_info "--- Ingress Status ---"
    kubectl get ing -n minecraft-cluster || log_warning "No Ingress resources found."

    echo ""
    log_info "--- Events (last 5 minutes) ---"
    kubectl get events -n minecraft-cluster --sort-by='.lastTimestamp' --since=5m

    echo ""
    log_info "--- Unhealthy Pods ---"
    UNHEALTHY_PODS=$(kubectl get pods -n minecraft-cluster --field-selector=status.phase!=Running -o wide --no-headers || true)
    if [ -z "$UNHEALTHY_PODS" ]; then
        log_success "All pods are in 'Running' state."
    else
        log_warning "The following pods are not in 'Running' state:"
        echo "$UNHEALTHY_PODS"
    fi
}


# Main deployment function
main() {
    log_info "Starting Minecraft Cluster deployment..."

    # Ensure kubeconfig is set up
    setup_kubeconfig

    # Create namespace
    log_info "Creating 'minecraft-cluster' namespace..."
    kubectl apply -f namespace.yaml
    log_success "Namespace 'minecraft-cluster' created/ensured."

    # Deploy Redis Cluster
    deploy_redis_cluster
    wait_for_deployment redis-cluster minecraft-cluster 600 # 10 minutes timeout

    # Deploy MongoDB
    log_info "Deploying MongoDB..."
    kubectl apply -f mongodb.yaml
    log_success "MongoDB deployment initiated."
    wait_for_deployment mongodb-deployment minecraft-cluster

    # Deploy Player Service
    log_info "Deploying Player Service..."
    kubectl apply -f player-service.yaml
    log_success "Player Service deployment initiated."
    wait_for_deployment player-service minecraft-cluster

    # Deploy Game Service
    log_info "Deploying Game Service..."
    kubectl apply -f game-service.yaml
    log_success "Game Service deployment initiated."
    wait_for_deployment game-service minecraft-cluster

    # Deploy Gate Proxy
    log_info "Deploying Gate Proxy..."
    kubectl apply -f gate.yaml
    log_success "Gate Proxy deployment initiated."
    wait_for_deployment gate-proxy-deployment minecraft-cluster

    # Deploy Minestom Servers (assuming this is a DaemonSet or Deployment)
    log_info "Deploying Minestom Servers..."
    kubectl apply -f minestom.yaml
    log_success "Minestom Servers deployment initiated."
    wait_for_deployment minestom-server minecraft-cluster # Adjust label selector if different

    log_success "Minecraft Cluster deployment complete!"
    echo ""
    verify_deployment
    echo ""
    log_info "=== Useful Commands ===
To get the external IP for the Gate Proxy:
  kubectl get svc gate-proxy-service -n minecraft-cluster

To connect to Redis cluster:
  export REDIS_PASSWORD=\$(kubectl get secret --namespace minecraft-cluster redis-cluster -o jsonpath=\"{.data.redis-password}\" | base64 -d)
  kubectl run --namespace minecraft-cluster redis-client --rm --tty -i --restart='Never' --image docker.io/bitnami/redis-cluster:7.2.4-debian-12-r11 -- bash
  # Inside container: redis-cli -c -a \$REDIS_PASSWORD -h redis-cluster

To check IPv6 connectivity:
  kubectl exec -it deployment/redis-cluster -n minecraft-cluster -- redis-cli cluster nodes
"
}

# Function to setup kubeconfig
setup_kubeconfig() {
    if kubectl config current-context &> /dev/null; then
        log_success "Kubeconfig is already set up and a current context is active."
    else
        log_warning "No current kubeconfig context found. Attempting to use K3s default."
        if [ -f "/etc/rancher/k3s/k3s.yaml" ]; then
            export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
            log_info "KUBECONFIG set to /etc/rancher/k3s/k3s.yaml."
            if ! kubectl config current-context &> /dev/null; then
                log_error "Failed to set KUBECONFIG with /etc/rancher/k3s/k3s.yaml. Check permissions or if K3s is running."
                log_info "Hint: You might need to run 'sudo chmod 644 /etc/rancher/k3s/k3s.yaml' or 'sudo chown \$USER:\$USER /etc/rancher/k3s/k3s.yaml'"
                exit 1
            else
                log_success "Kubeconfig successfully set using K3s default."
            fi
        else
            log_error "No kubeconfig found and K3s default '/etc/rancher/k3s/k3s.yaml' does not exist."
            log_error "Please ensure kubectl is configured to connect to your Kubernetes cluster."
            exit 1
        fi
    fi
}


# Handle script arguments
case "${1:-}" in
    --dry-run)
        log_info "Dry run mode - showing what would be deployed"
        kubectl apply --dry-run=client -f namespace.yaml
        # For dry-run, we won't actually call deploy_redis_cluster, but we can simulate the values file creation
        create_redis_standard_values # Ensure the IPv4 values file is created for inspection
        log_info "Dry-run for Redis values file: redis-standard-values.yaml"
        # If you want to see the IPv6 values in dry-run for completeness, uncomment the next two lines
        # create_redis_ipv6_values
        # log_info "Dry-run for Redis values file: redis-ipv6-values.yaml"
        ;;
    --verify)
        log_info "Verification mode - checking deployment status"
        verify_deployment
        ;;
    --help|-h)
        echo "Usage: $0 [OPTIONS]"
        echo "Options:"
        echo "  --dry-run    Show what would be deployed without applying"
        echo "  --verify     Check the status of deployed resources"
        echo "  --help|-h    Display this help message"
        ;;
    *)
        main
        ;;
esac