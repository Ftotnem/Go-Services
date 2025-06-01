#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Deploying Kubernetes resources for Minecraft Cluster..."

# Create Namespace
echo "Creating namespace 'minecraft-cluster'..."
kubectl apply -f namespace.yaml

# Deploy Redis Cluster
echo "Deploying Redis Cluster (StatefulSet and Service)..."
kubectl apply -f redis-cluster.yaml

echo "Waiting for Redis cluster pods to be ready..."
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=redis-cluster --timeout=300s

echo "Redis cluster pods are ready. You might need to manually form the cluster using redis-cli if not using an operator."
echo "Example (run from one of the redis pods):"
echo "kubectl exec -it redis-cluster-0 -n minecraft-cluster -- redis-cli --cluster create \
redis-cluster-0.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
redis-cluster-1.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
redis-cluster-2.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
redis-cluster-3.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
redis-cluster-4.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
redis-cluster-5.redis-cluster.minecraft-cluster.svc.cluster.local:6379 \
--cluster-replicas 1" # Adjust --cluster-replicas as needed

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
echo "To get the external IP for the Gate Proxy (if Metallb is configured and has allocated an IP):"
echo "kubectl get svc gate-proxy-service -n minecraft-cluster"