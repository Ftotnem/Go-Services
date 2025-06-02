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

# Check if the Redis secret already exists to retrieve the password for upgrades
REDIS_SECRET_NAME="redis-cluster"
REDIS_PASSWORD_KEY="redis-password" # This is the key used by the Bitnami chart for the password
REDIS_CURRENT_PASSWORD=""

if kubectl get secret "$REDIS_SECRET_NAME" -n minecraft-cluster &> /dev/null; then
    echo "Existing Redis secret '$REDIS_SECRET_NAME' found. Retrieving current password..."
    REDIS_CURRENT_PASSWORD=$(kubectl get secret "$REDIS_SECRET_NAME" -n minecraft-cluster -o jsonpath="{.data.$REDIS_PASSWORD_KEY}" | base64 -d)
    echo "Using existing Redis password for upgrade."
else
    echo "No existing Redis secret '$REDIS_SECRET_NAME' found. Helm will generate a new password on first install."
fi

# Construct the --set argument for the password.
# IMPORTANT FIX: Removed redundant quotes around $REDIS_CURRENT_PASSWORD
REDIS_PASSWORD_SET_ARG=""
if [ -n "$REDIS_CURRENT_PASSWORD" ]; then
    # Corrected: Remove the inner \" and rely on Helm to handle quoting
    REDIS_PASSWORD_SET_ARG="--set password=$REDIS_CURRENT_PASSWORD"
fi

# Use helm upgrade --install to create or update the Redis Cluster
# If REDIS_CURRENT_PASSWORD is set, it will be passed to ensure password persistence.
# Otherwise, the chart will generate a new one on initial install.
echo "Installing/Upgrading Redis Cluster Helm release..."
helm upgrade --install redis-cluster bitnami/redis-cluster \
  --namespace minecraft-cluster \
  --set auth.enabled=true \
  $REDIS_PASSWORD_SET_ARG \
  --wait \
  --timeout 600s

echo "Redis Cluster deployed successfully by Helm (password enabled)."
echo "Helm has handled waiting for pods and forming the cluster automatically."
echo "The Redis password is stored in the 'redis-cluster' secret under the key 'redis-password'."

# --- End Redis Cluster Deployment ---


# Deploy MongoDB
echo "Deploying MongoDB (PersistentVolumeClaim, Deployment, Service)..."
kubectl apply -f mongodb.yaml

echo "Waiting for MongoDB pod to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=mongodb --timeout=300s

# Deploy Player Service
echo "Deploying Player Service (Deployment and Service)..."
# Ensure player-service.yaml references the 'redis-cluster' secret and 'redis-password' key
kubectl apply -f player-service.yaml

echo "Waiting for Player Service pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=player-service --timeout=300s

# Deploy Game Service
echo "Deploying Game Service (Deployment and Service)..."
# Ensure game-service.yaml references the 'redis-cluster' secret and 'redis-password' key
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
# Fetching the password from the secret created by the Helm chart
echo "export REDIS_PASSWORD=$(kubectl get secret --namespace minecraft-cluster redis-cluster -o jsonpath=\"{.data.redis-password}\" | base64 -d)"
echo "kubectl run --namespace minecraft-cluster minecraft-redis-cluster-client --rm --tty -i --restart='Never' --image docker.io/bitnami/redis-cluster:7.2.4-debian-12-r11 -- bash"
echo "  # Once inside the container, run:"
echo "  # redis-cli -c -a \$REDIS_PASSWORD -h redis-cluster"
