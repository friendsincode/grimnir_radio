# Grimnir Radio Kubernetes Deployment

This directory contains Kubernetes manifests for deploying Grimnir Radio to a Kubernetes cluster.

## Prerequisites

- Kubernetes cluster (1.24+)
- `kubectl` configured to access your cluster
- Ingress controller (nginx recommended)
- Storage class supporting `ReadWriteMany` for media files
- (Optional) cert-manager for automatic TLS certificates

## Quick Start

### 1. Create Secrets

**Important:** Do not use the example secrets in production!

```bash
# Create namespace first
kubectl apply -f namespace.yaml

# Create secrets from literal values
kubectl create secret generic grimnir-secrets \
  --namespace=grimnir-radio \
  --from-literal=POSTGRES_PASSWORD=$(openssl rand -base64 32) \
  --from-literal=REDIS_PASSWORD=$(openssl rand -base64 32) \
  --from-literal=GRIMNIR_JWT_SIGNING_KEY=$(openssl rand -base64 32) \
  --from-literal=GRIMNIR_DB_DSN="host=grimnir-postgres port=5432 user=grimnir password=$(openssl rand -base64 32) dbname=grimnir sslmode=disable" \
  --from-literal=GRIMNIR_REDIS_ADDR="grimnir-redis:6379" \
  --from-literal=GRIMNIR_REDIS_PASSWORD="$(openssl rand -base64 32)"
```

### 2. Customize Configuration

Edit `configmap.yaml` and `grimnir.yaml` to match your environment:

- Set `GRIMNIR_LEADER_ELECTION_ENABLED=true` for multi-replica deployments
- Adjust resource requests/limits based on your workload
- Update storage sizes in PVCs
- Configure Ingress hostname in `ingress.yaml`

### 3. Deploy

Using kubectl:
```bash
kubectl apply -f .
```

Or using kustomize:
```bash
kubectl apply -k .
```

### 4. Verify Deployment

```bash
# Check pods
kubectl get pods -n grimnir-radio

# Check services
kubectl get svc -n grimnir-radio

# Check logs
kubectl logs -n grimnir-radio -l app=grimnir-radio --follow

# Check leader status
kubectl exec -n grimnir-radio deployment/grimnir-radio -- curl -s http://localhost:8080/healthz
```

### 5. Access the Application

Get the LoadBalancer external IP:
```bash
kubectl get svc grimnir-radio -n grimnir-radio
```

Or configure Ingress with your domain:
```bash
kubectl get ingress -n grimnir-radio
```

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     Ingress (nginx)                          │
│               radio.example.com → grimnir-radio              │
└────────────────────────┬─────────────────────────────────────┘
                         │
         ┌───────────────┴──────────────┬─────────────────┐
         │                              │                 │
    ┌────▼─────┐                  ┌────▼──────┐    ┌────▼──────┐
    │ Pod 1    │                  │  Pod 2    │    │  Pod 3    │
    │ (Leader) │                  │ (Follower)│    │ (Follower)│
    └────┬─────┘                  └────┬──────┘    └────┬──────┘
         │                              │                 │
         └──────────────────┬───────────┴─────────────────┘
                            │
              ┌─────────────┴──────────────┐
              │                            │
     ┌────────▼─────────┐        ┌────────▼─────────┐
     │  PostgreSQL      │        │      Redis       │
     │  StatefulSet     │        │   StatefulSet    │
     └──────────────────┘        └──────────────────┘
              │                            │
     ┌────────▼─────────┐        ┌────────▼─────────┐
     │    PVC (10Gi)    │        │    PVC (1Gi)     │
     └──────────────────┘        └──────────────────┘
```

## Configuration

### Replicas

The default deployment runs 3 replicas of Grimnir Radio with leader election enabled:
- 1 leader runs the scheduler
- All replicas serve API requests
- Automatic failover if leader crashes

To scale:
```bash
kubectl scale deployment grimnir-radio -n grimnir-radio --replicas=5
```

### Resources

Default resource allocation per pod:

| Component | Requests | Limits |
|-----------|----------|--------|
| Grimnir Radio | 256Mi / 250m CPU | 512Mi / 1 CPU |
| Media Engine | 512Mi / 500m CPU | 2Gi / 2 CPU |
| PostgreSQL | 256Mi / 250m CPU | 512Mi / 500m CPU |
| Redis | 128Mi / 100m CPU | 256Mi / 250m CPU |

Adjust in each YAML file based on your workload.

### Storage

- **PostgreSQL**: 10Gi (increase for large media libraries)
- **Redis**: 1Gi
- **Media Files**: 50Gi with `ReadWriteMany` access mode
- **Media Engine Cache**: 2Gi ephemeral storage

### Networking

Exposed services:
- **HTTP API**: Port 80/443 via Ingress
- **Metrics**: Port 9000 (Prometheus)
- **gRPC (internal)**: Media engine on port 9091

## Monitoring

### Prometheus

The deployment includes Prometheus metrics on port 9000:

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: grimnir-radio
  namespace: grimnir-radio
spec:
  selector:
    matchLabels:
      app: grimnir-radio
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

### Logging

View logs with:
```bash
# All pods
kubectl logs -n grimnir-radio -l app=grimnir-radio --all-containers=true --follow

# Specific pod
kubectl logs -n grimnir-radio grimnir-radio-xxxx-yyyy --follow

# Previous crashed container
kubectl logs -n grimnir-radio grimnir-radio-xxxx-yyyy --previous
```

## Troubleshooting

### Pods not starting

Check pod status:
```bash
kubectl describe pod -n grimnir-radio <pod-name>
```

Common issues:
- **ImagePullBackOff**: Build and push images to your container registry
- **PVC pending**: Ensure storage class is available
- **Secret not found**: Create secrets first

### Database connection errors

```bash
# Test PostgreSQL connectivity
kubectl run -it --rm debug --image=postgres:15-alpine --restart=Never -n grimnir-radio -- \
  psql -h grimnir-postgres -U grimnir -d grimnir

# Check PostgreSQL logs
kubectl logs -n grimnir-radio grimnir-postgres-0
```

### Leader election issues

```bash
# Check which pod is leader
for pod in $(kubectl get pods -n grimnir-radio -l app=grimnir-radio -o name); do
  echo -n "$pod: "
  kubectl exec -n grimnir-radio $pod -- curl -s http://localhost:8080/healthz | jq .leader
done

# Check Redis connectivity
kubectl exec -n grimnir-radio deployment/grimnir-radio -- nc -zv grimnir-redis 6379
```

### Media engine issues

```bash
# Check media engine health
kubectl exec -n grimnir-radio deployment/grimnir-mediaengine -- /usr/local/bin/mediaengine health

# Check gRPC connectivity from grimnir pod
kubectl exec -n grimnir-radio deployment/grimnir-radio -- nc -zv grimnir-mediaengine 9091
```

## Upgrades

Rolling update with zero downtime:

```bash
# Update image tag in deployment
kubectl set image deployment/grimnir-radio -n grimnir-radio \
  grimnir-radio=grimnir-radio:v1.1.0

# Watch rollout
kubectl rollout status deployment/grimnir-radio -n grimnir-radio

# Rollback if needed
kubectl rollout undo deployment/grimnir-radio -n grimnir-radio
```

## Backup

### Database

```bash
# Create backup
kubectl exec -n grimnir-radio grimnir-postgres-0 -- \
  pg_dump -U grimnir grimnir > grimnir-backup-$(date +%Y%m%d).sql

# Restore from backup
kubectl exec -i -n grimnir-radio grimnir-postgres-0 -- \
  psql -U grimnir grimnir < grimnir-backup-20260122.sql
```

### Media Files

Use your storage provider's snapshot feature or:

```bash
# Create a backup pod with access to PVC
kubectl run backup --image=alpine:latest -n grimnir-radio \
  --overrides='
{
  "spec": {
    "containers": [{
      "name": "backup",
      "image": "alpine:latest",
      "command": ["sleep", "3600"],
      "volumeMounts": [{
        "name": "media",
        "mountPath": "/media"
      }]
    }],
    "volumes": [{
      "name": "media",
      "persistentVolumeClaim": {
        "claimName": "grimnir-media-pvc"
      }
    }]
  }
}'

# Copy files out
kubectl cp -n grimnir-radio backup:/media ./media-backup/
```

## Production Recommendations

1. **Use external secret management**: HashiCorp Vault, AWS Secrets Manager, etc.
2. **Enable TLS**: Use cert-manager for automatic certificate management
3. **Set resource limits**: Based on load testing
4. **Enable horizontal pod autoscaling**: For API pods
5. **Use managed PostgreSQL**: AWS RDS, Google Cloud SQL, etc.
6. **Enable backup**: Automated database and media backups
7. **Monitoring**: Deploy Prometheus, Grafana, and alerting
8. **Network policies**: Restrict pod-to-pod communication
9. **Pod security policies**: Run as non-root, read-only filesystem
10. **Multi-AZ deployment**: For high availability

## Clean Up

Remove all resources:
```bash
kubectl delete namespace grimnir-radio
```

Or remove specific resources:
```bash
kubectl delete -f .
```
