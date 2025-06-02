#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Deploying Kubernetes resources for Minecraft Cluster..."

# Create Namespace
echo "Creating namespace 'minecraft-cluster'..."
kubectl apply -f namespace.yaml

# --- Deploy Redis Cluster using Bitnami Helm chart ---
echo "Deploying Redis Cluster using Bitnami Helm chart (with password)..."

# Add Bitnami Helm repository (if not already added)
helm repo add bitnami https://charts.bitnami.com/bitnami || echo "Bitnami repo already added."

# Update Helm repositories to ensure latest chart versions are available
helm repo update

# --- NEW: Forceful cleanup of previous Redis Helm release before re-install ---
echo "Attempting to uninstall existing Redis Cluster Helm release to ensure a clean slate..."
helm uninstall redis-cluster --namespace minecraft-cluster --no-hooks --wait || true # --no-hooks helps if hooks are stuck, --wait ensures it completes
echo "Existing Redis Cluster Helm release uninstalled (if it existed)."

# Generate a random password and store it in a Kubernetes Secret
# This ensures the password is managed by Kubernetes and is consistently used.
REDIS_SECRET_NAME="redis-cluster-password"
# --- NEW: Always recreate the secret to ensure it's fresh ---
echo "Creating/recreating Kubernetes secret '$REDIS_SECRET_NAME' for Redis password..."
# Delete the secret if it exists, then recreate it
kubectl delete secret "$REDIS_SECRET_NAME" -n minecraft-cluster --ignore-not-found=true
REDIS_GENERATED_PASSWORD=$(openssl rand -base64 12)
kubectl create secret generic "$REDIS_SECRET_NAME" \
    --from-literal=password="$REDIS_GENERATED_PASSWORD" \
    --namespace minecraft-cluster

# Install the Redis Cluster chart with the newly generated password
# Removed auth.existingSecret and auth.passwordKey as the chart creates its own secret 'redis-cluster'
# and directly setting the password is more reliable for 'requirepass'.
helm install redis-cluster bitnami/redis-cluster \
  --namespace minecraft-cluster \
  --set auth.enabled=true \
  --set auth.password="$REDIS_GENERATED_PASSWORD" \ # --- NEW: Directly set the password ---
  --wait \
  --timeout 600s

echo "Redis Cluster deployed successfully by Helm (password enabled)."
echo "Helm has handled waiting for pods and forming the cluster automatically."

# --- End Redis Cluster Deployment ---


# Deploy MongoDB
echo "Deploying MongoDB (PersistentVolumeClaim, Deployment, Service)..."
kubectl apply -f mongodb.yaml

echo "Waiting for MongoDB pod to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=mongodb --timeout=300s

# Deploy Player Service
echo "Deploying Player Service (Deployment and Service)..."
# IMPORTANT: You will need to apply player-service.yaml AFTER this script,
# or add the kubectl apply -f player-service.yaml line here
# but ensure player-service.yaml is updated with the REDIS_PASSWORD env var.
# For now, keep it manual until player-service.yaml is updated below.
kubectl apply -f player-service.yaml

echo "Waiting for Player Service pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=player-service --timeout=300s

# Deploy Game Service
echo "Deploying Game Service (Deployment and Service)..."
# IMPORTANT: You will need to apply game-service.yaml AFTER this script,
# or add the kubectl apply -f game-service.yaml line here
# but ensure game-service.yaml is updated with the REDIS_PASSWORD env var.
# For now, keep it manual until game-service.yaml is updated below.
kubectl apply -f game-service.yaml

echo "Waiting for Game Service pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=game-service --timeout=300s

# Deploy Gate Proxy
echo "Deploying Gate Proxy (Deployment and LoadBalancer Service)..."
kubectl apply -f gate.yaml

echo "Waiting for Gate Proxy pods to be ready..."
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
echo "To connect to the Redis cluster client (password needed):"
echo "export REDIS_PASSWORD=$(kubectl get secret --namespace minecraft-cluster $REDIS_SECRET_NAME -o jsonpath=\"{.data.password}\" | base64 -d)"
echo "kubectl run --namespace minecraft-cluster minecraft-redis-cluster-client --rm --tty -i --restart='Never' --image docker.io/bitnami/redis-cluster:7.2.4-debian-12-r11 -- bash"
echo "  # Once inside the container, run:"
echo "  # redis-cli -c -a $REDIS_PASSWORD -h redis-cluster"