# Custom Scheduler Deployment Guide

This document details how to deploy and verify the Custom Scheduler plugin.

## Prerequisites

1. **Kubernetes cluster** (v1.20+)
2. **Node Feature Discovery (NFD)** installed
3. **kubectl** configured and able to access the cluster
4. **Docker** or container runtime (for building images)
5. **Make** (optional but recommended)

## Deployment Steps

### 1. Verify NFD Installation

First confirm that NFD is properly installed:

```bash
# Check if nfd-master Pod is running
kubectl get pods -A | grep nfd-master

# Check if NodeFeatureGroup CRD exists
kubectl get crd nodefeaturegroups.nfd.k8s-sigs.io
```

### 2. Build Docker Image

```bash
# Build for amd64 (default)
make docker-build

# Build for arm64
make docker-build ARCH=arm64

# Build with custom registry and tag
REGISTRY=docker.io/leoyy6 IMAGE_NAME=custom-scheduler IMAGE_TAG=v1.0.0 make docker-build

# Build arm64 with custom parameters
REGISTRY=docker.io/leoyy6 IMAGE_NAME=custom-scheduler IMAGE_TAG=v1.0.0 ARCH=arm64 make docker-build
```

**Note**: Use `make help` to see all available build options.

### 3. Push Image to Registry

```bash
# Push amd64 image (default)
make docker-push

# Push arm64 image
make docker-push ARCH=arm64

# Push with custom registry and tag
REGISTRY=docker.io/leoyy6 IMAGE_NAME=custom-scheduler IMAGE_TAG=v1.0.0 make docker-push

# Push arm64 with custom parameters
REGISTRY=docker.io/leoyy6 IMAGE_NAME=custom-scheduler IMAGE_TAG=v1.0.0 ARCH=arm64 make docker-push
```

**Note**: If using a private registry, login first:
```bash
docker login docker.io/leoyy6
```

**Manual Push (Alternative)**:
```bash
# Push image manually (same tag for all architectures)
docker push docker.io/leoyy6/custom-scheduler:v1.0.0
```

### 3.1 Load Image to Kind (Optional)

If you are using a local [Kind](https://kind.sigs.k8s.io/) cluster for testing, you can load the built image directly into the cluster instead of pushing to a remote registry:

```bash
# Load image to your Kind cluster
kind load docker-image docker.io/leoyy6/custom-scheduler:v1.0.0

# Or if you built with different tag, use that tag
kind load docker-image <your-image-tag>
```

**Note**: Make sure your Kind cluster is running and `kind` CLI is installed.

### 4. Update Image in Deployment File

Edit `deploy/deployment.yaml` to update the image address:

```yaml
containers:
- name: scheduler
  image: docker.io/leoyy6/custom-scheduler:v1.0.0  # Update here
```

### 5. Deploy to Kubernetes

```bash
# Delete existing deployment (if any)
kubectl delete deployment custom-scheduler -n custom-scheduler

# Use Makefile for one-click deployment (will automatically build and push image)
make deploy

# Or deploy manually
kubectl apply -k deploy/

# Update (if needed)
kubectl rollout restart deployment custom-scheduler -n custom-scheduler
```

### 6. Verify Deployment

```bash
# Check Pod status
kubectl get pods -n custom-scheduler

# View Pod logs
kubectl logs -n custom-scheduler -l app=custom-scheduler

# Check ServiceAccount and RBAC
kubectl get sa -n custom-scheduler
kubectl get clusterrole custom-scheduler
kubectl get clusterrolebinding custom-scheduler
```

## Verification Steps

### 1. Check Scheduler Running Status

```bash
# Check if scheduler Pod is Running
kubectl get pods -n custom-scheduler

# View detailed logs
kubectl logs -n custom-scheduler -l app=custom-scheduler -f
```

Expected output should include:
- Scheduler successfully started
- Plugins successfully registered
- No error messages

### 2. Verify Plugin Registration

```bash
# Check scheduler configuration
kubectl get configmap scheduler-config -n custom-scheduler -o yaml

# View scheduler process information (if metrics are enabled)
kubectl port-forward -n custom-scheduler deployment/custom-scheduler 10259:10259
curl http://localhost:10259/metrics | grep scheduler
```

### 3. Test Scheduling Functionality
#### 3.1 Prepare Test Image:

1. Prepare remote test image

2. Attach compatibility artifact to image:
```bash
# attach compatibility artifact to test image
oras attach --insecure --artifact-type application/vnd.nfd.image-compatibility.v1alpha1 \
  docker.io/leoyy6/alpine-simple-test:v7 \
  scripts/compatibility-artifact.yaml:application/vnd.nfd.image-compatibility.spec.v1alpha1+yaml

# View artifacts in image
oras discover --format json --plain-http docker.io/leoyy6/alpine-simple-test:v7

# View specific manifest
oras manifest fetch --format json --plain-http docker.io/leoyy6/alpine-simple-test:v7@sha256:<digest>

# Delete specific artifact (need to login first, e.g., oras login docker.io)
oras manifest delete -f docker.io/leoyy6/alpine-simple-test:v7@sha256:<digest>
```

#### 3.2 Create Test Pod

Create a test Pod using the custom scheduler:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-scheduler-pod
  namespace: default
  labels:
    app: test-scheduler
spec:
  schedulerName: custom-scheduler
  containers:
  - name: alpine
    image: docker.io/leoyy6/alpine-simple-test:v7
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
      limits:
        memory: "128Mi"
        cpu: "500m"
  restartPolicy: Never
EOF
```

Use script to create pod:
```bash
kubectl apply -f scripts/test-pod.yaml
```

#### 3.3 Check Pod Scheduling Status

```bash
# View Pod status
kubectl get pod test-scheduler-pod -o wide

# View Pod events
kubectl describe pod test-scheduler-pod

# View scheduler logs
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50
```

#### 3.4 Verify NodeFeatureGroup Creation

```bash
# Get nfd-master namespace
NFD_NS=$(kubectl get pods -A -l app.kubernetes.io/name=node-feature-discovery,role=master -o jsonpath='{.items[0].metadata.namespace}')

# View created NodeFeatureGroup CRs
kubectl get nodefeaturegroups -n $NFD_NS

# View NodeFeatureGroup details
kubectl get nodefeaturegroups -n $NFD_NS -o yaml
```

### 4. Verify Node Filtering Functionality

#### 4.1 Check Compatible Node Set

```bash
# View compatible node information in scheduler logs
kubectl logs -n custom-scheduler -l app=custom-scheduler | grep "compatible nodes"

# View node list in NodeFeatureGroup status
kubectl describe nodefeaturegroup <nfg_name> -n $NFD_NS
```

### 5. Verify TTL Cleanup Mechanism

```bash
# Delete test Pod
kubectl delete pod test-scheduler-pod

# Wait a few seconds and check if NodeFeatureGroup is automatically deleted
sleep 10
kubectl get nodefeaturegroups -n $NFD_NS
```

## Uninstallation

```bash
# Use Makefile
make undeploy

# Or delete manually
kubectl delete -f deploy/
```

## Next Steps

- View [README.md](../README.md) to understand how the plugin works
- Check scheduler logs for debugging
- Adjust scheduler configuration according to actual needs
