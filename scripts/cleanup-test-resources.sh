#!/bin/bash

# 清理测试资源脚本

set -e

echo "清理测试资源..."

# 获取 nfd-master namespace
NFD_NS=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null || echo "node-feature-discovery")

# 删除测试 Pod
echo "删除测试 Pod..."
kubectl delete pod test-scheduler-pod -n default --ignore-not-found=true
kubectl delete pod test-pod -n default --ignore-not-found=true
kubectl delete pod test-pod-incompatible -n default --ignore-not-found=true

# 删除临时 NodeFeatureGroup CRs
echo "删除临时 NodeFeatureGroup CRs..."
kubectl delete nodefeaturegroups -n $NFD_NS -l managed-by=ImageCompatibilityFilter,temporary=true --ignore-not-found=true

# 等待清理完成
sleep 3

echo "清理完成！"
