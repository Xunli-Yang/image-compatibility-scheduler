# Quick Start Guide

## One-Click Deployment and Verification

### 1. Build and Deploy

```bash
# Build Docker image (amd64 by default)
make docker-build

# Push image (if using remote registry)
make docker-push

# Alternative for Kind users: load image to Kind cluster
kind load docker-image docker.io/leoyy6/custom-scheduler:v1.0.0

# Deploy to Kubernetes
make deploy
```

**Note**: For building other architectures (e.g., arm64), use `make docker-build ARCH=arm64`. See `make help` for more options.

### 2. Verify Deployment

```bash
# Check scheduler Pod status
kubectl get pods -n custom-scheduler

# View scheduler logs
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50
```

### 3. Manual Testing

Create a test Pod:

```bash
kubectl apply -f scripts/test-pod.yaml
```

Check scheduling status:

```bash
# View Pod status
kubectl get pod test-scheduler-pod

# View scheduler logs
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50

# View NodeFeatureGroup CRs
NFD_NS=$(kubectl get pods -A -l app.kubernetes.io/name=node-feature-discovery,role=master -o jsonpath='{.items[0].metadata.namespace}')
kubectl get nodefeaturegroups -n $NFD_NS
```

## Common Issues

### Q: How to check if the plugin is working?

```bash
kubectl logs -n custom-scheduler -l app=custom-scheduler -f
```

Check scheduler logs for:
- "filter pod" messages
- "compatible nodes" related information
- No error messages

### Q: Pod stays in Pending state?

```bash
# View Pod status
kubectl get pod test-scheduler-pod

# View scheduler logs
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50

# View NodeFeatureGroup CRs
NFD_NS=$(kubectl get pods -A -l app.kubernetes.io/name=node-feature-discovery,role=master -o jsonpath='{.items[0].metadata.namespace}')
kubectl get nodefeaturegroups -n $NFD_NS
```

### Q: How to update the scheduler?

```bash
# Rebuild image
make docker-build

# Push new image (or load to Kind for local testing)
make docker-push
# Alternative for Kind: kind load docker-image docker.io/leoyy6/custom-scheduler:v1.0.0

# Restart Deployment
kubectl rollout restart deployment/custom-scheduler -n custom-scheduler
```

## Next Steps

- View [DEPLOYMENT.md](docs/DEPLOYMENT.md) for detailed deployment steps
- View [README.md](README.md) to understand how the plugin works
