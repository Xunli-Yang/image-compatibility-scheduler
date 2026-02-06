# 快速开始指南

## 一键部署和验证

### 1. 构建和部署

```bash
# 构建 Docker 镜像
make docker-build

# 推送镜像（如果使用远程 registry）
make docker-push

# 部署到 Kubernetes
make deploy
```

### 2. 快速验证

运行验证脚本：

```bash
./scripts/verify-deployment.sh
```

### 3. 手动测试

创建测试 Pod：

```bash
kubectl apply -f scripts/test-pod.yaml
```

检查调度状态：

```bash
# 查看 Pod 状态
kubectl get pod test-scheduler-pod

# 查看调度器日志
kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=50

# 查看 NodeFeatureGroup CRs
NFD_NS=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.namespace}')
kubectl get nodefeaturegroups -n $NFD_NS
```

### 4. 清理测试资源

```bash
./scripts/cleanup-test-resources.sh
```

## 常见问题

### Q: 如何查看调度器日志？

```bash
kubectl logs -n custom-scheduler -l app=custom-scheduler -f
```

### Q: 如何检查插件是否工作？

查看调度器日志中是否有：
- "filter pod" 消息
- "compatible nodes" 相关信息
- 没有错误信息

### Q: Pod 一直处于 Pending 状态？

1. 检查调度器是否运行：
   ```bash
   kubectl get pods -n custom-scheduler
   ```

2. 查看 Pod 事件：
   ```bash
   kubectl describe pod <pod-name>
   ```

3. 检查调度器日志：
   ```bash
   kubectl logs -n custom-scheduler -l app=custom-scheduler
   ```

### Q: 如何更新调度器？

```bash
# 重新构建镜像
make docker-build

# 推送新镜像
make docker-push

# 重启 Deployment
kubectl rollout restart deployment/custom-scheduler -n custom-scheduler
```

## 下一步

- 查看 [DEPLOYMENT.md](docs/DEPLOYMENT.md) 了解详细部署步骤
- 查看 [README.md](README.md) 了解插件工作原理
