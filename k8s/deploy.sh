#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Deploying Kubernetes resources for Minecraft Cluster..."

# Create Namespace
echo "Creating namespace 'minecraft-cluster'..."
kubectl apply -f namespace.yaml

# IMPORTANT SECURITY NOTE: Password authentication for Redis is being explicitly disabled.
# This is INSECURE for production environments and should ONLY be done in trusted, isolated development environments.

# --- Deploy Redis Cluster using Bitnami Helm chart ---
echo "Deploying Redis Cluster using Bitnami Helm chart (without password)..."

# Add Bitnami Helm repository (if not already added)
# Removed the --allow-repo-urls flag
helm repo add bitnami https://charts.bitnami.com/bitnami || echo "Bitnami repo already added."

# Update Helm repositories to ensure latest chart versions are available
helm repo update

# Install the Redis Cluster chart, disabling password authentication
# --wait: Helm will wait for all resources in the release to be ready (including Redis pods)
# --timeout: Maximum time to wait for the deployment to succeed
# --set auth.enabled=false: This is the key change to disable password authentication.
helm install redis-cluster bitnami/redis-cluster \
  --namespace minecraft-cluster \
  --set auth.enabled=false \
  --wait \
  --timeout 600s

echo "Redis Cluster deployed successfully by Helm (password disabled)."
echo "Helm has handled waiting for pods and forming the cluster automatically."

# --- End Redis Cluster Deployment ---


# Deploy MongoDB
echo "Deploying MongoDB (PersistentVolumeClaim, Deployment, Service)..."
kubectl apply -f mongodb.yaml

echo "Waiting for MongoDB pod to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=mongodb --timeout=300s

# Deploy Player Service
echo "Deploying Player Service (Deployment and Service)..."
kubectl apply -f player-service.yaml

echo "Waiting for Player Service pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=player-service --timeout=300s

# Deploy Game Service
echo "Deploying Game Service (Deployment and Service)..."
kubectl apply -f game-service.yaml

echo "Waiting for Game Service pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=game-service --timeout=300s

# Deploy Gate Proxy
echo "Deploying Gate Proxy (Deployment and LoadBalancer Service)..."
kubectl apply -f gate.yaml

echo "Waiting for Gate Proxy pods to be ready..."bt
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=gate-proxy --timeout=300s

# Deploy Minestom Servers
echo "Deploying Minestom Servers (Deployment)..."
kubectl apply -f minestom.yaml

echo "Waiting for Minestom Server pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=minestom-server --timeout=300s

echo "All services deployed successfully!"
echo ""
echo "To get the external IP for the Gate Proxy (if K3s/MetalLB has allocated an IP):"
echo "kubectl get svc gate-proxy-service -n minecraft-cluster"
echo ""
echo "To connect to the Redis cluster client (no password needed):"
echo "kubectl run --namespace minecraft-cluster minecraft-redis-cluster-client --rm --tty -i --restart='Never' --image docker.io/bitnami/redis-cluster:7.2.4-debian-12-r11 -- bash"
echo "  # Once inside the container, run:"
echo "  # redis-cli -c -h redis-cluster"