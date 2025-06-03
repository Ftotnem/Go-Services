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
    
    cat > /tmp/redis-ipv6-values.yaml << EOF
# IPv6 and dual-stack configuration for Redis Cluster
global:
  storageClass: "local-path"

redis:
  # Configure Redis to bind to both IPv4 and IPv6
  configmap: |
    bind 0.0.0.0 ::
    port 6379
    protected-mode yes
    tcp-keepalive 60
    tcp-backlog 511
    cluster-enabled yes
    cluster-config-file nodes.conf
    cluster-node-timeout 5000
    appendonly yes
    # Prefer hostname for cluster communication
    cluster-prefer-endpoint-ip no
  
  # Environment variables for better IPv6 support
  extraEnvVars:
    - name: REDIS_CLUSTER_ANNOUNCE_HOSTNAME
      value: "true"
    - name: REDIS_CLUSTER_PREFERRED_ENDPOINT_TYPE
      value: "hostname"
    - name: REDIS_CLUSTER_DYNAMIC_IPS
      value: "no"

# Service configuration for dual-stack
service:
  type: ClusterIP
  ipFamilyPolicy: PreferDualStack
  ipFamilies:
    - IPv6
    - IPv4

# Headless service for cluster formation
headlessService:
  ipFamilyPolicy: PreferDualStack
  ipFamilies:
    - IPv6
    - IPv4

# Pod configuration
podSecurityContext:
  sysctls:
    - name: net.ipv6.conf.all.disable_ipv6
      value: "0"

# Network policy (disabled for IPv6 compatibility)
networkPolicy:
  enabled: false

# Persistence settings
persistence:
  enabled: true
  storageClass: "local-path"
  size: 8Gi

# Resource limits (adjust as needed)
resources:
  limits:
    memory: 512Mi
    cpu: 500m
  requests:
    memory: 256Mi
    cpu: 100m
EOF
}

# Function to create standard Redis values (IPv4)
create_redis_standard_values() {
    log_info "Creating standard Redis configuration..."
    
    cat > /tmp/redis-standard-values.yaml << EOF
# Standard configuration for Redis Cluster
global:
  storageClass: "local-path"

# Persistence settings
persistence:
  enabled: true
  storageClass: "local-path"
  size: 8Gi

# Resource limits (adjust as needed)
resources:
  limits:
    memory: 512Mi
    cpu: 500m
  requests:
    memory: 256Mi
    cpu: 100m

# Network policy
networkPolicy:
  enabled: false
EOF
}

# Function to deploy Redis with appropriate configuration
deploy_redis_cluster() {
    log_info "Deploying Redis Cluster using Bitnami Helm chart..."

    # Add Bitnami Helm repository (if not already added)
    helm repo add bitnami https://charts.bitnami.com/bitnami 2>/dev/null || log_info "Bitnami repo already added."

    # Update Helm repositories
    helm repo update

    # Check for existing Redis secret
    REDIS_SECRET_NAME="redis-cluster"
    REDIS_PASSWORD_KEY="redis-password"
    REDIS_CURRENT_PASSWORD=""

    if kubectl get secret "$REDIS_SECRET_NAME" -n minecraft-cluster &> /dev/null; then
        log_info "Existing Redis secret '$REDIS_SECRET_NAME' found. Retrieving current password..."
        REDIS_CURRENT_PASSWORD=$(kubectl get secret "$REDIS_SECRET_NAME" -n minecraft-cluster -o jsonpath="{.data.$REDIS_PASSWORD_KEY}" | base64 -d)
        log_success "Using existing Redis password for upgrade."
    else
        log_info "No existing Redis secret found. Helm will generate a new password on first install."
    fi

    # Construct password argument
    REDIS_PASSWORD_SET_ARG=""
    if [ -n "$REDIS_CURRENT_PASSWORD" ]; then
        REDIS_PASSWORD_SET_ARG="--set auth.password=$REDIS_CURRENT_PASSWORD"
    fi

    # Determine which values file to use based on IPv6 support
    VALUES_FILE="/tmp/redis-standard-values.yaml"
    if check_ipv6_support; then
        create_redis_ipv6_values
        VALUES_FILE="/tmp/redis-ipv6-values.yaml"
        log_info "Using IPv6-enabled Redis configuration"
    else
        create_redis_standard_values
        log_info "Using standard Redis configuration"
    fi

    # Deploy Redis cluster
    log_info "Installing/Upgrading Redis Cluster Helm release..."
    helm upgrade --install redis-cluster bitnami/redis-cluster \
      --namespace minecraft-cluster \
      --values "$VALUES_FILE" \
      --set auth.enabled=true \
      $REDIS_PASSWORD_SET_ARG \
      --wait \
      --timeout 600s

    # Clean up temporary files
    rm -f /tmp/redis-ipv6-values.yaml /tmp/redis-standard-values.yaml

    log_success "Redis Cluster deployed successfully!"
}

# Function to wait for deployment with better feedback
wait_for_deployment() {
    local app_name=$1
    local timeout=${2:-300}
    
    log_info "Waiting for $app_name pods to be ready (timeout: ${timeout}s)..."
    
    if kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app="$app_name" --timeout="${timeout}s"; then
        log_success "$app_name pods are ready!"
    else
        log_error "Timeout waiting for $app_name pods to be ready"
        log_info "Pod status for $app_name:"
        kubectl get pods -n minecraft-cluster -l app="$app_name"
        return 1
    fi
}

# Function to verify deployment
verify_deployment() {
    log_info "Verifying deployment status..."
    
    echo ""
    log_info "=== Deployment Summary ==="
    kubectl get pods -n minecraft-cluster -o wide
    echo ""
    kubectl get services -n minecraft-cluster
    echo ""
    
    # Check if any pods are not running
    NOT_RUNNING=$(kubectl get pods -n minecraft-cluster --field-selector=status.phase!=Running --no-headers 2>/dev/null | wc -l)
    if [ "$NOT_RUNNING" -gt 0 ]; then
        log_warning "Some pods are not in Running state:"
        kubectl get pods -n minecraft-cluster --field-selector=status.phase!=Running
    fi
}

# Main deployment function
main() {
    log_info "Starting Kubernetes deployment for Minecraft Cluster..."

    # Create Namespace
    log_info "Creating namespace 'minecraft-cluster'..."
    kubectl apply -f namespace.yaml

    # Deploy Redis Cluster
    deploy_redis_cluster

    # Deploy MongoDB
    log_info "Deploying MongoDB..."
    kubectl apply -f mongodb.yaml
    wait_for_deployment "mongodb"

    # Deploy Player Service
    log_info "Deploying Player Service..."
    kubectl apply -f player-service.yaml
    wait_for_deployment "player-service"

    # Deploy Game Service
    log_info "Deploying Game Service..."
    kubectl apply -f game-service.yaml
    wait_for_deployment "game-service"

    # Deploy Gate Proxy
    log_info "Deploying Gate Proxy..."
    kubectl apply -f gate.yaml
    wait_for_deployment "gate-proxy"

    # Deploy Minestom Servers
    log_info "Deploying Minestom Servers..."
    kubectl apply -f minestom.yaml
    wait_for_deployment "minestom-server"

    # Verify deployment
    verify_deployment

    log_success "All services deployed successfully!"
    
    echo ""
    log_info "=== Useful Commands ==="
    echo "To get the external IP for the Gate Proxy:"
    echo "  kubectl get svc gate-proxy-service -n minecraft-cluster"
    echo ""
    echo "To connect to Redis cluster:"
    echo "  export REDIS_PASSWORD=\$(kubectl get secret --namespace minecraft-cluster redis-cluster -o jsonpath=\"{.data.redis-password}\" | base64 -d)"
    echo "  kubectl run --namespace minecraft-cluster redis-client --rm --tty -i --restart='Never' --image docker.io/bitnami/redis-cluster:7.2.4-debian-12-r11 -- bash"
    echo "  # Inside container: redis-cli -c -a \$REDIS_PASSWORD -h redis-cluster"
    echo ""
    echo "To check IPv6 connectivity:"
    echo "  kubectl exec -it deployment/redis-cluster -n minecraft-cluster -- redis-cli cluster nodes"
}

# Handle script arguments
case "${1:-}" in
    --dry-run)
        log_info "Dry run mode - showing what would be deployed"
        kubectl apply --dry-run=client -f namespace.yaml
        check_ipv6_support
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
        echo "  --help, -h   Show this help message"
        echo ""
        echo "Default: Deploy all resources"
        ;;
    *)
        main
        ;;
esac