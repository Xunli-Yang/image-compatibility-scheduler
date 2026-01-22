#!/bin/bash

# Custom Scheduler 部署验证脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

# 检查命令是否存在
check_command() {
    if ! command -v $1 &> /dev/null; then
        print_error "$1 未安装，请先安装"
        exit 1
    fi
    print_success "$1 已安装"
}

# 检查 Kubernetes 连接
check_k8s_connection() {
    if ! kubectl cluster-info &> /dev/null; then
        print_error "无法连接到 Kubernetes 集群"
        exit 1
    fi
    print_success "Kubernetes 集群连接正常"
}

# 检查 NFD 安装
check_nfd() {
    print_info "检查 NFD 安装..."
    
    # 检查 nfd-master Pod
    if ! kubectl get pods -A -l app=nfd-master &> /dev/null; then
        print_error "未找到 nfd-master Pod，请先安装 NFD"
        exit 1
    fi
    
    NFD_POD=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    NFD_NS=$(kubectl get pods -A -l app=nfd-master -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
    
    if [ -z "$NFD_POD" ]; then
        print_error "nfd-master Pod 未运行"
        exit 1
    fi
    
    print_success "NFD 已安装 (Pod: $NFD_POD, Namespace: $NFD_NS)"
    
    # 检查 NodeFeatureGroup CRD
    if ! kubectl get crd nodefeaturegroups.nfd.k8s-sigs.io &> /dev/null; then
        print_error "NodeFeatureGroup CRD 不存在"
        exit 1
    fi
    print_success "NodeFeatureGroup CRD 存在"
    
    export NFD_NS
}

# 检查调度器部署
check_scheduler_deployment() {
    print_info "检查调度器部署..."
    
    # 检查 Namespace
    if ! kubectl get namespace custom-scheduler &> /dev/null; then
        print_error "custom-scheduler namespace 不存在"
        exit 1
    fi
    print_success "Namespace custom-scheduler 存在"
    
    # 检查 ServiceAccount
    if ! kubectl get sa custom-scheduler -n custom-scheduler &> /dev/null; then
        print_error "ServiceAccount custom-scheduler 不存在"
        exit 1
    fi
    print_success "ServiceAccount custom-scheduler 存在"
    
    # 检查 ClusterRole
    if ! kubectl get clusterrole custom-scheduler &> /dev/null; then
        print_error "ClusterRole custom-scheduler 不存在"
        exit 1
    fi
    print_success "ClusterRole custom-scheduler 存在"
    
    # 检查 Deployment
    if ! kubectl get deployment custom-scheduler -n custom-scheduler &> /dev/null; then
        print_error "Deployment custom-scheduler 不存在"
        exit 1
    fi
    
    # 检查 Pod 状态
    POD_STATUS=$(kubectl get pods -n custom-scheduler -l app=custom-scheduler -o jsonpath='{.items[0].status.phase}' 2>/dev/null)
    if [ "$POD_STATUS" != "Running" ]; then
        print_error "调度器 Pod 未运行 (状态: $POD_STATUS)"
        kubectl get pods -n custom-scheduler -l app=custom-scheduler
        exit 1
    fi
    print_success "调度器 Pod 运行正常"
    
    # 检查日志中是否有错误
    LOGS=$(kubectl logs -n custom-scheduler -l app=custom-scheduler --tail=20 2>&1)
    if echo "$LOGS" | grep -i "error\|fatal\|panic" &> /dev/null; then
        print_warning "调度器日志中发现错误，请检查："
        echo "$LOGS" | grep -i "error\|fatal\|panic"
    else
        print_success "调度器日志无错误"
    fi
}

# 测试调度功能
test_scheduling() {
    print_info "测试调度功能..."
    
    # 创建测试 Pod
    TEST_POD_NAME="test-scheduler-$(date +%s)"
    kubectl apply -f - <<EOF > /dev/null
apiVersion: v1
kind: Pod
metadata:
  name: $TEST_POD_NAME
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
    
    print_info "等待 Pod 调度..."
    sleep 5
    
    # 检查 Pod 状态
    POD_STATUS=$(kubectl get pod $TEST_POD_NAME -n default -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    
    if [ "$POD_STATUS" == "Pending" ]; then
        print_warning "Pod 处于 Pending 状态，检查原因..."
        kubectl describe pod $TEST_POD_NAME -n default | grep -A 10 "Events:"
    elif [ "$POD_STATUS" == "Running" ]; then
        print_success "Pod 已成功调度并运行"
    else
        print_warning "Pod 状态: $POD_STATUS"
    fi
    
    # 检查 NodeFeatureGroup 是否创建
    sleep 3
    NFG_COUNT=$(kubectl get nodefeaturegroups -n $NFD_NS -l managed-by=ImageCompatibilityFilter --no-headers 2>/dev/null | wc -l)
    if [ "$NFG_COUNT" -gt 0 ]; then
        print_success "NodeFeatureGroup CRs 已创建 ($NFG_COUNT 个)"
    else
        print_warning "未找到 NodeFeatureGroup CRs"
    fi
    
    # 清理测试 Pod
    kubectl delete pod $TEST_POD_NAME -n default --ignore-not-found=true > /dev/null
    print_info "测试 Pod 已清理"
}

# 验证 TTL 清理
test_ttl_cleanup() {
    print_info "验证 TTL 清理机制..."
    
    # 创建测试 Pod
    TEST_POD_NAME="test-ttl-$(date +%s)"
    kubectl apply -f - <<EOF > /dev/null
apiVersion: v1
kind: Pod
metadata:
  name: $TEST_POD_NAME
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
    
    sleep 5
    
    # 记录 NodeFeatureGroup 数量
    NFG_BEFORE=$(kubectl get nodefeaturegroups -n $NFD_NS -l managed-by=ImageCompatibilityFilter --no-headers 2>/dev/null | wc -l)
    print_info "删除 Pod 前 NodeFeatureGroup 数量: $NFG_BEFORE"
    
    # 删除 Pod
    kubectl delete pod $TEST_POD_NAME -n default > /dev/null
    
    # 等待清理
    print_info "等待 TTL 清理（10秒）..."
    sleep 10
    
    # 检查 NodeFeatureGroup 是否被清理
    NFG_AFTER=$(kubectl get nodefeaturegroups -n $NFD_NS -l managed-by=ImageCompatibilityFilter --no-headers 2>/dev/null | wc -l)
    print_info "删除 Pod 后 NodeFeatureGroup 数量: $NFG_AFTER"
    
    if [ "$NFG_AFTER" -lt "$NFG_BEFORE" ]; then
        print_success "TTL 清理机制工作正常"
    else
        print_warning "TTL 清理可能未生效，请检查 OwnerReference 配置"
    fi
}

# 主函数
main() {
    echo "=========================================="
    echo "Custom Scheduler 部署验证"
    echo "=========================================="
    echo ""
    
    # 检查前置条件
    print_info "检查前置条件..."
    check_command kubectl
    check_k8s_connection
    check_nfd
    echo ""
    
    # 检查部署
    print_info "检查调度器部署..."
    check_scheduler_deployment
    echo ""
    
    # 测试功能
    print_info "测试调度功能..."
    test_scheduling
    echo ""
    
    # 测试 TTL
    print_info "测试 TTL 清理..."
    test_ttl_cleanup
    echo ""
    
    echo "=========================================="
    print_success "验证完成！"
    echo "=========================================="
}

# 运行主函数
main
