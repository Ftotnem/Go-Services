#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Deploying Kubernetes resources for Minecraft Cluster (Fixed Redis)..."

# Create Namespace
echo "Creating namespace 'minecraft-cluster'..."
kubectl apply -f namespace.yaml

# Deploy Fixed Redis Cluster (with automatic clustering)
echo "Deploying Fixed Redis Cluster (StatefulSet, Service, and ConfigMap)..."
kubectl apply -f redis-cluster.yaml

echo "Waiting for Redis cluster pods to be ready..."
# Wait for pods to be ready according to their readiness probes
kubectl wait --namespace minecraft-cluster --for=condition=ready pod -l app=redis-cluster --timeout=600s

# Give the cluster formation process time to complete
echo "Waiting for Redis cluster formation to complete..."
sleep 30

# Verify cluster is working
echo "Verifying Redis cluster status..."
kubectl exec redis-cluster-0 -n minecraft-cluster -- redis-cli cluster info
kubectl exec redis-cluster-0 -n minecraft-cluster -- redis-cli cluster nodes

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