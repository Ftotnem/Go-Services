# Go-Services

go-services/
├── go.mod                 <-- Your single go.mod file lives here
├── go.sum
│
├── player/                <-- 'player' service application (main package)
│   └── main.go
│   └── ... (other player service specific code)
│
├── game/                  <-- 'game' service application (main package)
│   └── main.go
│   └── ... (other game service specific code)
│
└── shared/
    ├── api/               <-- Reusable 'api' package (server, client, middleware, response)
    │   ├── client.go
    │   ├── server.go
    │   ├── middleware.go
    │   └── response.go
    │
    ├── registry/          <-- Reusable 'registry' package (Redis client, types, constants)
    │   ├── client.go
    │   ├── types.go
    │   └── constants.go
    │
    └── services/          <-- Reusable 'services' package (clients for other services)
        ├── gameclient.go
        └── playerclient.go

Okay, thank you for clarifying those critical details! This helps immensely in drawing a precise map for your architecture, especially concerning Minestom routing and self-hosted Kubernetes.

Let's break this down into a more accurate and sequential plan.

Understanding Your Refined Vision
Top-Level Entry Point: HAProxy (External, Bare-Metal)

Deployment: Runs on your public-facing Ubuntu server.
Purpose: The only internet-exposed component. Load balances all incoming player connections.
Target: Exclusively directs traffic to Gate-Proxy instances.
Dynamic Registration: Gate-Proxy instances will dynamically register and heartbeat with this HAProxy using its Runtime API.
Gate-Proxy Layer (Go, K8s Pods)

Role: Your custom Minecraft proxy.
Network: Runs within your Kubernetes cluster.
Public Access: Receives traffic from the external HAProxy.
Internal API: Has its own HTTP API (e.g., port 8080 internally).
Minestom Routing (CRITICAL):
Gate-Proxies will NOT use HAProxy for Minestom load balancing.
Gate-Proxies implement custom logic (e.g., choosing a server with more players) to route to Minestom Servers.
This implies Gate-Proxies need to know the list of available Minestom servers and their status/player counts.
Backend Communication: Needs to securely talk to Game-Service and Player-Service (HTTP APIs).
Minestom Server Layer (Java, K8s Pods)

Role: Minecraft game servers.
Network: Runs within your Kubernetes cluster.
Registration: Minestom Servers must register and heartbeat their status (including player count) to a centralized service registry that Gate-Proxies can query. They will not register directly with Gate-Proxies in a distributed K8s setup.
Backend Communication: Needs to securely talk to Game-Service and Player-Service (HTTP APIs).
Backend Services (Go, K8s Pods)

Game-Service (Go, HTTP API, interacts with Redis Cluster).
Player-Service (Go, HTTP API, interacts with MongoDB Player Service).
Security: These services must be internal-only and completely shielded from the internet.
Internal Access: Accessed by Gate-Proxies and Minestom Servers.
Internal Load Balancing: You initially suggested another HAProxy here. In a K8s context, Kubernetes ClusterIP Services handle this natively and are typically preferred. We can use these for Game-Service and Player-Service to simplify.
Data Stores (K8s Pods - StatefulSets)

Redis Cluster: Used by Game-Service (and potentially others).
MongoDB: Used by Player-Service.
Deployment: These will need to be deployed as StatefulSets in Kubernetes for persistent data, which is more complex than simple stateless deployments.
Self-Hosted Kubernetes:

Your entire application (Gate-Proxy, Minestom, Game-Service, Player-Service, Redis, Mongo) will run inside your Kubernetes cluster on your Ubuntu server.
The only thing outside the K8s cluster will be the External HAProxy.
Network & Deployment Map for Self-Hosted K8s
This map integrates your bare-metal HAProxy with your K8s cluster.

+-----------------------------------------------------------------------+
|                        Your Ubuntu Server (Host OS)                   |
|                                                                       |
|  +-----------------------------------------------------------------+  |
|  |                        Internet / Players                       |  |
|  +-----------------------------+-----------------------------------+  |
|                                | TCP (25565)                                |
|                                V                                          |
|  +-----------------------------------------------------------------+  |
|  |           HAProxy (External Load Balancer) - Bare Metal       |  |
|  |           (Runs directly on Host OS, Public IP: <SERVER_IP>)  |  |
|  |           (Admin Socket: /var/run/haproxy/external_admin.sock) |  |
|  +-----------------------------+-----------------------------------+  |
|                                | TCP (25565, to NodePort)                   |
|                                V                                          |
|  +-----------------------------------------------------------------+  |
|  |                 Kubernetes Cluster (Minikube / K3s / Kubeadm)   |  |
|  |  (Runs within your Ubuntu Server - e.g., via Docker Desktop, VM, or containers) |
|  |                                                                 |  |
|  |  +-----------------------------------------------------------+  |  |
|  |  |                         K8s Node 1 (<Node1_IP>)           |  |  |
|  |  |                                                           |  |  |
|  |  |  +-----------------+  +-----------------+  +----------+  |  |  |
|  |  |  | Gate-Proxy Pod  |  | Minestom Pod    |  | Game-Svc |  |  |  |
|  |  |  | ( listens 25566)|  | ( listens 25565) |  | (8082)   |  |  |  |
|  |  |  +-----------------+  +-----------------+  +----------+  |  |  |
|  |  |  +-----------------+  +-----------------+  +----------+  |  |  |
|  |  |  |  K8s ClusterIP  |  | K8s ClusterIP   |  | K8s Svc  |  |  |  |
|  |  |  |  Service (8081) |  |  Service (8082) |  | for Redis|  |  |  |
|  |  |  | (Player-Service)|  | (Game-Service)  |  | (6379)   |  |  |  |
|  |  |  +-------^---------+  +----------^------+  +----------+  |  |  |
|  |  |          |                     |                           |  |  |
|  |  |          | (Internal K8s DNS: player-service:8081)         |  |  |
|  |  |          | (Internal K8s DNS: game-service:8082)           |  |  |
|  |  |          +-------------------------------------------------+  |  |
|  |  |                                                           |  |  |
|  |  |  +-------------------------------------------------------+  |  |
|  |  |  |                 K8s NodePort (for Gate-Proxy)         |  |  |
|  |  |  | (e.g., NodePort: 30000 -> containerPort: 25566)         |  |  |
|  |  |  +-------------------------------------------------------+  |  |
|  |  +-----------------------------------------------------------+  |  |
|  |                                                                 |  |
|  |  +-----------------------------------------------------------+  |  |
|  |  |         Internal Communication within K8s (e.g., Redis)   |  |  |
|  |  |                                                           |  |  |
|  |  |  +-----------------+  +-----------------+  +----------+  |  |  |
|  |  |  | Redis Cluster   |  | MongoDB         |  | Player-Svc|  |  |  |
|  |  |  | (K8s StatefulSet)|  | (K8s StatefulSet)|  | (8081)   |  |  |  |
|  |  |  +-----------------+  +-----------------+  +----------+  |  |  |
|  |  +-----------------------------------------------------------+  |  |
|  +-----------------------------------------------------------------+  |
+-----------------------------------------------------------------------+
Key Architectural Decisions Reflected in the Map:

Bare-Metal HAProxy for External LB: You explicitly stated "no cloud provider" and want HAProxy. This makes the most sense. It will sit outside the K8s cluster and forward to a NodePort Service of your Gate-Proxy within K8s.
No Internal HAProxy: Kubernetes ClusterIP Services will handle internal load balancing between your Game-Service, Player-Service, Gate-Proxy, and Minestom pods. This is the native, simpler K8s way.
Centralized Minestom Registry (e.g., Redis): Since Gate-Proxies have custom Minestom routing logic, they need a source of truth for Minestom server addresses and player counts. Redis is excellent for this. Minestom servers publish their status to Redis, and Gate-Proxies subscribe/query Redis.
Security: Only the external HAProxy is exposed. All other services communicate internally within the K8s cluster via secure, internal IPs and DNS names.

