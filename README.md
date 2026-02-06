# Custom Scheduler with Image Compatibility Plugin

A Kubernetes custom scheduler that filters nodes based on image compatibility using Node Feature Discovery (NFD).

## Features

- **Image Compatibility Filtering**: Filters nodes based on container image compatibility using NodeFeatureGroup CRs
- **Dynamic Namespace Discovery**: Automatically discovers nfd-master namespace
- **TTL-based Cleanup**: Temporary NodeFeatureGroup CRs are automatically cleaned up via OwnerReference

## Prerequisites

- Kubernetes cluster (v1.20+)
- Node Feature Discovery (NFD) installed
- Go 1.25+
- Docker (for building images)

## Building

### Build Binary

```bash
make build
```

### Build Docker Image

```bash
make docker-build
```

Or with custom registry:

```bash
REGISTRY=my-registry.io IMAGE_NAME=my-scheduler make docker-build
```

## Deployment

### Quick Start

```bash
# 1. Build Docker image
make docker-build

# 2. Push image (if using remote registry)
make docker-push

# 3. Deploy to Kubernetes
make deploy

# 4. Verify deployment
./scripts/verify-deployment.sh
```

详细部署步骤请参考 [DEPLOYMENT.md](docs/DEPLOYMENT.md)

快速开始指南请参考 [QUICKSTART.md](QUICKSTART.md)

### Manual Deployment

```bash
kubectl apply -f deploy/
```

### Undeploy

```bash
make undeploy
```

## Configuration

The scheduler configuration is stored in `deploy/configmap.yaml`. You can customize:

- Scheduler name
- Plugin configuration
- Leader election settings

## Plugin Details

### ImageCompatibilityFilter Plugin

The plugin implements the Filter extension point and:

1. Creates temporary NodeFeatureGroup CRs for each container image in the Pod
2. Runs nfd-master to update NodeFeatureGroup status with matching nodes
3. Computes the intersection of compatible nodes across all images
4. Filters nodes that are not compatible with all images

### Namespace Discovery

The plugin automatically discovers the nfd-master namespace by:

1. Searching common namespaces (node-feature-discovery, kube-system, default)
2. If not found, searching all namespaces for pods with label `app=nfd-master`

## Development

### Run Tests

```bash
make test
```

### Format Code

```bash
make fmt
```

### Lint

```bash
make lint
```

## Verification

### Automated Verification

运行验证脚本进行自动化验证：

```bash
./scripts/verify-deployment.sh
```

### Manual Verification

1. **检查调度器状态**:
   ```bash
   kubectl get pods -n custom-scheduler
   kubectl logs -n custom-scheduler -l app=custom-scheduler
   ```

2. **测试调度功能**:
   ```bash
   kubectl apply -f scripts/test-pod.yaml
   kubectl get pod test-scheduler-pod
   ```

3. **检查 NodeFeatureGroup CRs**:
   ```bash
   NFD_NS=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.namespace}')
   kubectl get nodefeaturegroups -n $NFD_NS
   ```

详细验证步骤请参考 [DEPLOYMENT.md](docs/DEPLOYMENT.md#验证步骤)

## License

[Your License Here]
