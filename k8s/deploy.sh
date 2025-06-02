#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Deploying Kubernetes resources for Minecraft Cluster..."

# Create Namespace
echo "Creating namespace 'minecraft-cluster'..."
kubectl apply -f namespace.yaml

# IMPORTANT SECURITY NOTE: Passwords are hardcoded as per user request.
# This is INSECURE for production environments.

# Deploy Redis Cluster
echo "Deploying Redis Cluster (StatefulSet, Service, and ConfigMap)..."
kubectl apply -f redis-cluster.yaml

echo "Waiting for Redis cluster pods to be ready..."
# Wait for pods to be ready according to their readiness probes
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=redis-cluster --timeout=300s

# Give Redis servers a moment to fully initialize inside pods after they are "ready"
echo "Giving Redis servers a moment to initialize for clustering..."
sleep 15 # Increased sleep for safer cluster creation

echo "Attempting to form Redis cluster if not already formed..."
# Get a random pod to check cluster info - ensure it's one of the actual cluster pods
REDIS_POD_NAME=$(kubectl get pod -l app=redis-cluster -n minecraft-cluster -o jsonpath='{.items[0].metadata.name}')

# Check if cluster is already formed by querying any redis pod
if kubectl exec -it "$REDIS_POD_NAME" -n minecraft-cluster -- redis-cli cluster info | grep -q "cluster_state:ok"; then
  echo "Redis cluster already formed. Skipping cluster creation."
else
  echo "Forming new Redis cluster..."
  # Construct the list of nodes for cluster creation. Use headless service FQDNs.
  REDIS_NODES=""
  for i in $(seq 0 5); do
    # Using the headless service FQDN as redis-cli can resolve it to the pod IP
    REDIS_NODES+="redis-cluster-$i.redis-cluster.minecraft-cluster.svc.cluster.local:6379 "
  done

  # Execute cluster creation command from the first pod
  # --cluster-yes accepts the configuration without prompt
  kubectl exec -it redis-cluster-0 -n minecraft-cluster -- redis-cli --cluster create $REDIS_NODES --cluster-replicas 1 --cluster-yes

  echo "Verifying Redis cluster status after creation (wait 5s)..."
  sleep 5
  kubectl exec -it redis-cluster-0 -n minecraft-cluster -- redis-cli cluster info
fi

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