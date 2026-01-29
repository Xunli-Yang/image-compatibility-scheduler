# Custom Scheduler 部署指南

本文档详细说明如何部署和验证 Custom Scheduler 插件。

## 前置条件

1. **Kubernetes 集群** (v1.20+)
2. **Node Feature Discovery (NFD)** 已安装
3. **kubectl** 已配置并可以访问集群
4. **Docker** 或容器运行时（用于构建镜像）
5. **Make** (可选，但推荐)

## 部署步骤

### 1. 验证 NFD 安装

首先确认 NFD 已正确安装：

```bash
# 检查 nfd-master Pod 是否运行
kubectl get pods -A | grep nfd-master

# 检查 NodeFeatureGroup CRD 是否存在
kubectl get crd nodefeaturegroups.nfd.k8s-sigs.io
```

### 2. 构建 Docker 镜像

```bash
# 使用默认配置构建
make docker-build

# 或指定自定义 registry 和 tag
REGISTRY=docker.io/leoyy6 IMAGE_NAME=custom-scheduler IMAGE_TAG=v1.0.0 make docker-build
```

### 3. 推送镜像到 Registry

```bash
# 使用默认配置推送
make docker-push

# 或手动推送
docker push docker.io/leoyy6/custom-scheduler:v1.0.0
```

**注意**: 如果使用私有 registry，需要先登录：
```bash
docker login docker.io/leoyy6
```

### 4. 更新部署文件中的镜像

编辑 `deploy/deployment.yaml`，更新镜像地址：

```yaml
containers:
- name: scheduler
  image: docker.io/leoyy6/custom-scheduler:v1.0.0  # 更新这里
```

### 5. 部署到 Kubernetes


```bash
#删除现有部署（如果有）
kubectl delete deployment custom-scheduler -n custom-scheduler

# 使用 Makefile 一键部署（会自动构建和推送镜像）
make deploy

# 或手动部署
kubectl apply -f deploy/
```

### 6. 验证部署

```bash
# 检查 Pod 状态
kubectl get pods -n custom-scheduler

# 查看 Pod 日志
kubectl logs -n custom-scheduler -l app=custom-scheduler

# 检查 ServiceAccount 和 RBAC
kubectl get sa -n custom-scheduler
kubectl get clusterrole custom-scheduler
kubectl get clusterrolebinding custom-scheduler
```

## 验证步骤

### 1. 检查调度器运行状态

```bash
# 查看调度器 Pod 是否 Running
kubectl get pods -n custom-scheduler

# 查看详细日志
kubectl logs -n custom-scheduler -l app=custom-scheduler -f
```

预期输出应该包含：
- 调度器成功启动
- 插件成功注册
- 没有错误信息

### 2. 验证插件注册

```bash
# 检查调度器配置
kubectl get configmap scheduler-config -n custom-scheduler -o yaml

# 查看调度器进程信息（如果启用了 metrics）
kubectl port-forward -n custom-scheduler deployment/custom-scheduler 10259:10259
curl http://localhost:10259/metrics | grep scheduler
```

### 3. 测试调度功能
#### 3.1 准备测试镜像：

1.准备好远端测试镜像

2.将兼容性工件attach到镜像：
```bash
oras attach --insecure --artifact-type application/vnd.nfd.image-compatibility.v1alpha1 \
  docker.io/leoyy6/alpine:3.19 \
  scripts/compatibility-artifact.yaml:application/vnd.nfd.image-compatibility.spec.v1alpha1+yaml
```

#### 3.2 创建测试 Pod

创建一个使用自定义调度器的测试 Pod：

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  schedulerName: custom-scheduler
  containers:
  - name: test-container
    image: nginx:latest
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
EOF
```

使用脚本创建pod：
```bash
kubectl apply -f scripts/test-pod.yaml
```


#### 3.3 检查 Pod 调度状态

```bash
# 查看 Pod 状态
kubectl get pod test-pod -o wide

# 查看 Pod 事件
kubectl describe pod test-pod

# 查看调度器日志
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50
```

#### 3.4 验证 NodeFeatureGroup 创建

```bash
# 获取 nfd-master namespace
NFD_NS=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.namespace}')

# 查看创建的 NodeFeatureGroup CRs
kubectl get nodefeaturegroups -n $NFD_NS

# 查看 NodeFeatureGroup 详情
kubectl get nodefeaturegroups -n $NFD_NS -o yaml
```

### 4. 验证节点过滤功能

#### 4.1 检查兼容节点集合

```bash
# 查看调度器日志中的兼容节点信息
kubectl logs -n custom-scheduler -l app=custom-scheduler | grep "compatible nodes"

# 查看 NodeFeatureGroup status 中的节点列表
kubectl get nodefeaturegroups -n $NFD_NS -o jsonpath='{.items[*].status.nodes[*].name}'
```

#### 4.2 测试不兼容场景

创建一个使用不兼容镜像的 Pod（如果存在）：

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-incompatible
  namespace: default
spec:
  schedulerName: custom-scheduler
  containers:
  - name: test-container
    image: some-incompatible-image:latest
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
EOF
```

检查 Pod 是否被正确拒绝：

```bash
kubectl get pod test-pod-incompatible
kubectl describe pod test-pod-incompatible | grep -i "unschedulable\|compatible"
```

### 5. 验证 TTL 清理机制

```bash
# 删除测试 Pod
kubectl delete pod test-pod

# 等待几秒后检查 NodeFeatureGroup 是否被自动删除
sleep 10
kubectl get nodefeaturegroups -n $NFD_NS
```

### 6. 性能验证

```bash
# 监控调度器资源使用
kubectl top pod -n custom-scheduler

# 查看调度器 metrics（如果启用）
kubectl port-forward -n custom-scheduler deployment/custom-scheduler 10259:10259
curl http://localhost:10259/metrics
```

## 故障排查

### 问题 1: Pod 无法启动

```bash
# 检查 Pod 状态
kubectl describe pod -n custom-scheduler -l app=custom-scheduler

# 检查事件
kubectl get events -n custom-scheduler --sort-by='.lastTimestamp'
```

### 问题 2: 调度器无法发现 nfd-master

```bash
# 检查 nfd-master 是否存在
kubectl get pods -A -l app=nfd-master

# 检查调度器日志中的 namespace 发现信息
kubectl logs -n custom-scheduler -l app=custom-scheduler | grep -i "namespace\|nfd-master"
```

### 问题 3: 权限问题

```bash
# 验证 RBAC 配置
kubectl auth can-i create nodefeaturegroups --as=system:serviceaccount:custom-scheduler:custom-scheduler -n node-feature-discovery

# 检查 ClusterRole
kubectl describe clusterrole custom-scheduler
```

### 问题 4: 镜像拉取失败

```bash
# 检查镜像是否存在
docker pull docker.io/leoyy6/custom-scheduler:v1.0.0

# 检查 ImagePullSecrets（如果使用私有 registry）
kubectl get secret -n custom-scheduler
```

## 卸载

```bash
# 使用 Makefile
make undeploy

# 或手动删除
kubectl delete -f deploy/
```

## 下一步

- 查看 [README.md](../README.md) 了解插件工作原理
- 查看调度器日志进行调试
- 根据实际需求调整调度器配置
