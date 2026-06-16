# Proposal C 架构设计

## 整体架构图

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                    Kubernetes Cluster                            │
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                         Control Plane Components                          │   │
│  │                                                                           │   │
│  │  ┌─────────────┐         ┌──────────────────┐         ┌──────────────┐  │   │
│  │  │   Webhook   │         │   nfd-master     │         │   Scheduler  │  │   │
│  │  │  (Mutating) │         │                  │         │   Plugin     │  │   │
│  │  └──────┬──────┘         └────────┬─────────┘         └──────┬───────┘  │   │
│  │         │                         │                          │           │   │
│  │         │ 1.Create ICQ            │ 3.Update status          │ 4.Read    │   │
│  │         │                         │                          │  status   │   │
│  │         ▼                         ▼                          ▼           │   │
│  │  ┌──────────────────────────────────────────────────────────────────┐   │   │
│  │  │                          etcd / API Server                        │   │   │
│  │  │                                                                    │   │   │
│  │  │  ┌─────────────────────┐              ┌────────────────────────┐ │   │   │
│  │  │  │ NodeFeatureGroup    │              │ ImageCompatibility     │ │   │   │
│  │  │  │ (admin pre-group)   │              │ Query (ICQ)            │ │   │   │
│  │  │  │                     │              │                        │ │   │   │
│  │  │  │ spec:               │              │ spec:                  │ │   │   │
│  │  │  │   featureGroupRules │              │   matchFeatures        │ │   │   │
│  │  │  │                     │              │                        │ │   │   │
│  │  │  │ status:             │              │ status:                │ │   │   │
│  │  │  │   nodes: [...]      │              │   compatibleNodes: [...]│ │   │   │
│  │  │  └─────────────────────┘              └────────────────────────┘ │   │   │
│  │  └──────────────────────────────────────────────────────────────────┘   │   │
│  │                                                                          │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                              Node Pool                                    │   │
│  │                                                                           │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      ┌─────────────┐ │   │
│  │  │   Node-1    │  │   Node-2    │  │   Node-3    │ ...  │   Node-N    │ │   │
│  │  │             │  │             │  │             │      │             │ │   │
│  │  │ NFD Worker  │  │ NFD Worker  │  │ NFD Worker  │      │ NFD Worker  │ │   │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘      └──────┬──────┘ │   │
│  │         │                │                │                     │        │   │
│  │         └────────────────┴────────────────┴─────────────────────┘        │   │
│  │                              │                                            │   │
│  │                              │ 2.Report NodeFeature                       │   │
│  │                              ▼                                            │   │
│  │                       ┌─────────────┐                                     │   │
│  │                       │ nfd-master  │                                     │   │
│  │                       └─────────────┘                                     │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                          External Components                              │   │
│  │                                                                           │   │
│  │  ┌─────────────────┐                    ┌─────────────────┐              │   │
│  │  │  OCI Registry   │                    │   Container     │              │   │
│  │  │                 │                    │    Images       │              │   │
│  │  │  - Image        │                    │                 │              │   │
│  │  │  - Artifact     │                    │  - app@sha256   │              │   │
│  │  │    (compat      │                    │  - init@sha256  │              │   │
│  │  │     metadata)   │                    │  - sidecar      │              │   │
│  │  └─────────────────┘                    └─────────────────┘              │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## 核心数据流

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                          Phase 1: Pod 创建阶段                                │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│   User/Controller                                                              │
│        │                                                                       │
│        │ kubectl apply -f pod.yaml                                            │
│        ▼                                                                       │
│   ┌─────────┐                                                                  │
│   │ API     │                                                                  │
│   │ Server  │                                                                  │
│   └────┬────┘                                                                  │
│        │                                                                       │
│        │ 1. Intercept Pod CREATE                                              │
│        ▼                                                                       │
│   ┌─────────────────────────────────────────────────────────────────────────┐ │
│   │                         Mutating Webhook                                 │ │
│   │                                                                          │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Step 1: Extract image references                                 │  │ │
│   │  │   - app@sha256:aaa                                               │  │ │
│   │  │   - init@sha256:bbb                                              │  │ │
│   │  │   - sidecar@sha256:ccc                                           │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Step 2: Check in-memory LRU cache                                │  │ │
│   │  │                                                                   │  │ │
│   │  │   Cache Key: image digest                                        │  │ │
│   │  │   Cache Value: compatibility rules                               │  │ │
│   │  │                                                                   │  │ │
│   │  │   if TTL expired:                                                │  │ │
│   │  │     HEAD registry → check artifact-digest                        │  │ │
│   │  │     if changed: refetch & update cache                           │  │ │
│   │  │                                                                   │  │ │
│   │  │   if cache miss:                                                 │  │ │
│   │  │     fetch OCI Artifact → parse → populate cache                  │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Step 3: Create ImageCompatibilityQuery CR                         │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each unique image digest:                                  │  │ │
│   │  │     if ICQ not exists:                                           │  │ │
│   │  │       create ICQ with spec.matchFeatures                         │  │ │
│   │  │       set refcount = 1                                           │  │ │
│   │  │     else:                                                        │  │ │
│   │  │       refcount++                                                 │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Step 4: Annotate Pod & Admit                                     │  │ │
│   │  │                                                                   │  │ │
│   │  │   Pod annotations:                                               │  │ │
│   │  │     nfd.k8s-sigs.io/image-digests: "sha256:aaa,sha256:bbb"      │  │ │
│   │  │                                                                   │  │ │
│   │  │   → Admit Pod                                                    │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   └─────────────────────────────────────────────────────────────────────────┘ │
│        │                                                                       │
│        │ 2. Pod created                                                       │
│        ▼                                                                       │
│   ┌─────────┐                                                                  │
│   │ etcd    │ ← ICQ CRs created                                                │
│   └─────────┘                                                                  │
│                                                                                │
└──────────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────┐
│                    Phase 2: NFD 特征收集与 status 计算                        │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│   ┌─────────────────────────────────────────────────────────────────────────┐ │
│   │                              Node Pool                                   │ │
│   │                                                                          │ │
│   │   Node-1          Node-2          Node-3          ...        Node-N      │ │
│   │     │                │                │                      │          │ │
│   │     │ Collect        │ Collect        │ Collect              │ Collect  │ │
│   │     │ features       │ features       │ features             │ features │ │
│   │     ▼                ▼                ▼                      ▼          │ │
│   │   NFD Worker       NFD Worker       NFD Worker            NFD Worker   │ │
│   │     │                │                │                      │          │ │
│   │     └────────────────┴────────────────┴──────────────────────┘          │ │
│   │                              │                                           │ │
│   │                              │ Report NodeFeature                        │ │
│   │                              ▼                                           │ │
│   └─────────────────────────────────────────────────────────────────────────┘ │
│                              │                                                 │
│                              ▼                                                 │
│   ┌─────────────────────────────────────────────────────────────────────────┐ │
│   │                            nfd-master                                    │ │
│   │                                                                          │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Task 1: Update NodeFeatureGroup (admin pre-group)                │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each pre-group:                                            │  │ │
│   │  │     collect node features                                        │  │ │
│   │  │     check homogeneity (internal, no exposed field)               │  │ │
│   │  │     update status.nodes                                          │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Task 2: Update ImageCompatibilityQuery                           │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each ICQ:                                                  │  │ │
│   │  │     ┌────────────────────────────────────────────────────────┐  │  │ │
│   │  │     │ Compute status.compatibleNodes                          │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ compatibleNodes = []                                     │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ // Use pre-group acceleration                           │  │  │ │
│   │  │     │ for each pre-group:                                      │  │  │ │
│   │  │     │   if pre-group homogeneous (internal check):            │  │  │ │
│   │  │     │     representative node match → O(1)                    │  │  │ │
│   │  │     │     if match → add all nodes to compatibleNodes         │  │  │ │
│   │  │     │   else:                                                  │  │  │ │
│   │  │     │     per-node match → O(group size)                      │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ // Handle ungrouped nodes                               │  │  │ │
│   │  │     │ ungroupedNodes = allNodes - ∪(pre-group nodes)          │  │  │ │
│   │  │     │ for each ungrouped node:                                │  │  │ │
│   │  │     │   per-node match                                        │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ status.compatibleNodes = compatibleNodes                │  │  │ │
│   │  │     └────────────────────────────────────────────────────────┘  │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Task 3: Detect Drift (optional)                                  │  │ │
│   │  │                                                                   │  │ │
│   │  │   oldNodes = old status.compatibleNodes                          │  │ │
│   │  │   newNodes = new status.compatibleNodes                          │  │ │
│   │  │   removedNodes = oldNodes - newNodes                             │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each removedNode:                                          │  │ │
│   │  │     check if Pod running on this node                            │  │ │
│   │  │     if yes:                                                      │  │ │
│   │  │       generate NodeCompatibilityDrift Event                      │  │ │
│   │  │       apply postDriftPolicy (ignore/taint/deschedule)            │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   └─────────────────────────────────────────────────────────────────────────┘ │
│                              │                                                 │
│                              │ Update ICQ status                              │
│                              ▼                                                 │
│   ┌─────────┐                                                                  │
│   │ etcd    │ ← ICQ status.compatibleNodes updated                             │
│   └─────────┘                                                                  │
│                                                                                │
└──────────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────┐
│                          Phase 3: 调度阶段                                    │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│   ┌─────────────────────────────────────────────────────────────────────────┐ │
│   │                         Scheduler                                        │ │
│   │                                                                          │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Prefilter Phase                                                   │  │ │
│   │  │                                                                   │  │ │
│   │  │   1. Read Pod annotations:                                        │  │ │
│   │  │      image-digests: "sha256:aaa,sha256:bbb"                       │  │ │
│   │  │                                                                   │  │ │
│   │  │   2. For each image digest:                                       │  │ │
│   │  │      query ICQ from informer cache                                │  │ │
│   │  │      if ICQ exists:                                               │  │ │
│   │  │        refcount++                                                 │  │ │
│   │  │        read status.compatibleNodes                                │  │ │
│   │  │      else:                                                        │  │ │
│   │  │        // Webhook failure fallback                                │  │ │
│   │  │        fetch OCI Artifact (sync)                                  │  │ │
│   │  │        create ICQ                                                 │  │ │
│   │  │        wait for status (requeue)                                  │  │ │
│   │  │                                                                   │  │ │
│   │  │   3. Store in SchedulingContext:                                  │  │ │
│   │  │      ICQ-aaa.compatibleNodes = [node-1..node-500]                │  │ │
│   │  │      ICQ-bbb.compatibleNodes = [node-1..node-800]                │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Filter Phase                                                      │  │ │
│   │  │                                                                   │  │ │
│   │  │   1. Compute intersection:                                        │  │ │
│   │  │      compatibleNodes = ICQ-aaa.nodes ∩ ICQ-bbb.nodes             │  │ │
│   │  │                    = [node-1..node-500]                           │  │ │
│   │  │                                                                   │  │ │
│   │  │   2. Filter candidate nodes:                                      │  │ │
│   │  │      filteredNodes = candidateNodes ∩ compatibleNodes             │  │ │
│   │  │                                                                   │  │ │
│   │  │   3. Return filtered nodes to scheduler framework                 │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Score / Reserve / Bind Phase                                      │  │ │
│   │  │                                                                   │  │ │
│   │  │   Score filtered nodes                                            │  │ │
│   │  │   Select best node                                                │  │ │
│   │  │   Bind Pod to node                                                │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                                │
└──────────────────────────────────────────────────────────────────────────────┘
```

## CRD 设计对比

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        NodeFeatureGroup (NFG)                                │
│                        管理员定义的节点分组                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  apiVersion: nfd.k8s-sigs.io/v1alpha1                                        │
│  kind: NodeFeatureGroup                                                      │
│  metadata:                                                                   │
│    name: gpu-kernel-group                                                    │
│    labels:                                                                   │
│      nfd.k8s-sigs.io/group-type: admin-pre-group                            │
│  spec:                                                                       │
│    featureGroupRules:                                                        │
│      - name: "gpu + kernel"                                                  │
│        matchFeatures:                                                        │
│          - feature: pci.device                                               │
│            matchExpressions:                                                 │
│              class: {op: In, value: ["0300"]}                                │
│          - feature: kernel.version                                           │
│            matchExpressions:                                                 │
│              major: {op: In, value: ["6"]}                                   │
│  status:                                                                     │
│    nodes:                                                                    │
│      - name: node-1                                                          │
│      - name: node-2                                                          │
│      - name: node-5                                                          │
│                                                                               │
│  生命周期: 长期存在，管理员手动管理                                             │
│  创建者: 集群管理员                                                            │
│  更新者: nfd-master (status.nodes)                                            │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                    ImageCompatibilityQuery (ICQ)                             │
│                    系统自动生成的镜像兼容性查询                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  apiVersion: nfd.k8s-sigs.io/v1alpha1                                        │
│  kind: ImageCompatibilityQuery                                               │
│  metadata:                                                                   │
│    name: icq-sha256-aaa123bb4567                                             │
│    annotations:                                                              │
│      nfd.k8s-sigs.io/image-ref: "registry.example.com/app@sha256:aaa..."    │
│      nfd.k8s-sigs.io/artifact-digest: "sha256:artifact-xxx"                 │
│      nfd.k8s-sigs.io/refcount: "3"                                           │
│      nfd.k8s-sigs.io/last-used: "2026-06-15T10:05:00Z"                      │
│  spec:                                                                       │
│    matchFeatures:                                                            │
│      - feature: kernel.version                                               │
│        matchExpressions:                                                     │
│          major: {op: In, value: ["6"]}                                       │
│      - feature: cpu.cpuid                                                    │
│        matchExpressions:                                                     │
│          AVX2: {op: Is, value: true}                                         │
│  status:                                                                     │
│    compatibleNodes:                                                          │
│      - name: node-1                                                          │
│      - name: node-2                                                          │
│      - name: node-5                                                          │
│    conditions:                                                               │
│      - type: Ready                                                           │
│        status: "True"                                                        │
│        lastTransitionTime: "2026-06-15T10:00:00Z"                           │
│                                                                               │
│  生命周期: 临时存在，自动 GC (refcount=0 + TTL)                               │
│  创建者: Webhook / Scheduler (降级模式)                                       │
│  更新者: nfd-master (status.compatibleNodes)                                  │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 性能优化策略

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          性能优化层次                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  Layer 1: Webhook 侧缓存                                                    │
│  ├─ 内存 LRU 缓存 (按 image digest 索引)                                    │
│  ├─ 1000 副本 Deployment → 仅 1 次 registry 拉取                            │
│  ├─ 惰性热加载 (TTL 过期时 HEAD registry)                                   │
│  └─ 延迟: 缓存命中 ~ms 级，未命中 ~100-500ms                                │
│                                                                               │
│  Layer 2: Image Digest 去重                                                   │
│  ├─ ICQ 按 image digest 命名 (icq-sha256-{prefix})                          │
│  ├─ 相同镜像复用同一个 ICQ                                                    │
│  ├─ Refcount 跟踪引用数                                                      │
│  └─ 1000 副本 → 1 个 ICQ CR                                                 │
│                                                                               │
│  Layer 3: 预分组加速 (nfd-master 侧)                                        │
│  ├─ 管理员定义 pre-group (NodeFeatureGroup)                                  │
│  ├─ 代表节点匹配: O(1) per group                                             │
│  ├─ 隐式同构性检测 (内部实现，不暴露字段)                                     │
│  ├─ 复杂度: O(G + U) vs O(N)                                                │
│  │   G = 分组数 (通常 10-50)                                                 │
│  │   U = 未分组节点数                                                        │
│  │   N = 总节点数                                                            │
│  └─ 10000 节点, 20 分组 → 从 O(10000) 降到 O(20)                           │
│                                                                               │
│  Layer 4: Informer 缓存 (Scheduler 侧)                                      │
│  ├─ Scheduler 通过 informer 缓存读取 ICQ status                             │
│  ├─ 无 API server 调用                                                       │
│  └─ 延迟: ~ms 级                                                             │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 故障降级流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          正常路径 vs 降级路径                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  正常路径 (Webhook 正常):                                                    │
│  ┌─────────┐      ┌─────────┐      ┌──────────┐      ┌─────────┐          │
│  │   Pod   │ ───▶ │ Webhook │ ───▶ │   ICQ    │ ───▶ │Scheduler│          │
│  │ CREATE  │      │ 解析OCI │      │ 创建     │      │ 读取    │          │
│  └─────────┘      └─────────┘      └──────────┘      │ status  │          │
│                                                        └─────────┘          │
│  延迟: ~ms 级 (缓存命中) / ~100-500ms (缓存未命中)                         │
│                                                                               │
│  降级路径 (Webhook 故障):                                                    │
│  ┌─────────┐      ┌─────────┐      ┌──────────┐      ┌─────────┐          │
│  │   Pod   │ ───▶ │ Webhook │ ───▶ │   Pod    │ ───▶ │Scheduler│          │
│  │ CREATE  │      │  失败   │      │ 创建     │      │ 降级    │          │
│  └─────────┘      └─────────┘      │ (无ICQ)  │      │ 解析OCI │          │
│                                     └──────────┘      │ 创建ICQ │          │
│                                                        │ 等待    │          │
│                                                        │ status  │          │
│                                                        └─────────┘          │
│  延迟: ~500ms-2s (首次) / ~ms 级 (后续相同镜像)                            │
│                                                                               │
│  Registry 不可达:                                                            │
│  ├─ Webhook 阶段: failurePolicy=Ignore → 放行 Pod，降级为 scheduler 解析    │
│  ├─ Scheduler 阶段: failurePolicy=Ignore → 跳过兼容性检查，继续调度         │
│  └─ failurePolicy=Fail → 标记 Pod unschedulable，等待恢复                   │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 漂移检测与处理

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          漂移检测流程                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  T1: Pod 调度到 node-50 (当时兼容)                                           │
│      ICQ.status.compatibleNodes = [node-1..node-100]                        │
│      Pod running on node-50 ✓                                               │
│                                                                               │
│  T2: node-50 内核升级，特征漂移                                              │
│      NFD worker 上报新特征                                                   │
│                                                                               │
│  T3: nfd-master 重算 ICQ status                                              │
│      ┌────────────────────────────────────────────────────────────────┐     │
│      │ old.compatibleNodes = [node-1..node-100]                       │     │
│      │ new.compatibleNodes = [node-1..node-49, node-51..node-100]    │     │
│      │                                                                 │     │
│      │ removedNodes = [node-50]                                        │     │
│      │                                                                 │     │
│      │ for node-50:                                                    │     │
│      │   check running Pods                                            │     │
│      │   found: my-pod using app@sha256:aaa                           │     │
│      │                                                                 │     │
│      │ generate Event:                                                 │     │
│      │   kind: Pod                                                     │     │
│      │   reason: NodeCompatibilityDrift                                │     │
│      │   message: "Pod my-pod is running on node-50 which no longer   │     │
│      │            satisfies compatibility requirements for image       │     │
│      │            app@sha256:aaa"                                       │     │
│      │                                                                 │     │
│      │ apply postDriftPolicy:                                          │     │
│      │   ignore (default) → no action                                  │     │
│      │   taint → add taint to node-50                                  │     │
│      │   deschedule → trigger descheduler                              │     │
│      └────────────────────────────────────────────────────────────────┘     │
│                                                                               │
│  T4: ICQ status updated                                                      │
│      Scheduler 下次调度时读到新的 compatibleNodes                             │
│      node-50 不再被选为兼容节点                                               │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 组件职责总结

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          组件职责矩阵                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  ┌─────────────────┬──────────────────────────────────────────────────────┐ │
│  │ Component       │ Responsibilities                                      │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ Mutating        │ • Intercept Pod CREATE                                │ │
│  │ Webhook         │ • Fetch OCI Artifact (with LRU cache)                 │ │
│  │                 │ • Parse compatibility metadata                        │ │
│  │                 │ • Create ImageCompatibilityQuery CR                   │ │
│  │                 │ • Annotate Pod with image digests                     │ │
│  │                 │ • Hot-reload (lazy check on TTL expiry)               │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ nfd-master      │ • Collect NodeFeature from NFD workers                │ │
│  │                 │ • Update NodeFeatureGroup status.nodes                │ │
│  │                 │ • Update ImageCompatibilityQuery status               │ │
│  │                 │   - Use pre-group acceleration                        │ │
│  │                 │   - Internal homogeneity detection                    │ │
│  │                 │   - Handle ungrouped nodes (residual set)             │ │
│  │                 │ • Detect drift (compare old/new status)               │ │
│  │                 │ • Generate NodeCompatibilityDrift events              │ │
│  │                 │ • Apply postDriftPolicy                               │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ Scheduler       │ • Read Pod annotations (image digests)                │ │
│  │ Plugin          │ • Query ICQ from informer cache                       │ │
│  │                 │ • Prefilter:                                          │ │
│  │                 │   - Check ICQ existence                               │ │
│  │                 │   - Fallback: sync OCI fetch + ICQ creation           │ │
│  │                 │   - Store compatibleNodes in SchedulingContext        │ │
│  │                 │ • Filter:                                             │ │
│  │                 │   - Compute intersection of all ICQ compatibleNodes   │ │
│  │                 │   - Filter candidate nodes                            │ │
│  │                 │ • Update ICQ refcount                                 │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ GC Controller   │ • Monitor ICQ refcount                                │ │
│  │ (optional)      │ • Delete ICQ when refcount=0 + TTL expired            │ │
│  │                 │ • Safety net for abnormal Pod exits                   │ │
│  └─────────────────┴──────────────────────────────────────────────────────┘ │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 关键设计决策

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          设计决策总结                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  1. 独立 CRD                                                                 │
│     ├─ NodeFeatureGroup: 管理员定义的节点分组                                 │
│     ├─ ImageCompatibilityQuery: 系统生成的兼容性查询                          │
│     └─ 理由: 语义清晰，RBAC 精确，生命周期独立                                │
│                                                                               │
│  2. 预分组优化                                                                │
│     ├─ 保留 admin pre-group 加速 nfd-master 计算                             │
│     ├─ 隐式同构性检测 (不引入 Homogeneous 字段)                               │
│     └─ 理由: 避免调度器复杂度，nfd-master 内部处理                            │
│                                                                               │
│  3. Webhook 预解析                                                            │
│     ├─ Mutating Webhook + 内存 LRU 缓存                                      │
│     ├─ 惰性热加载 (TTL 过期时检查)                                           │
│     └─ 理由: 移除 registry I/O 出调度热路径                                   │
│                                                                               │
│  4. Image Digest 去重                                                         │
│     ├─ ICQ 按 image digest 命名                                              │
│     ├─ Refcount 跟踪引用数                                                   │
│     └─ 理由: 最大化复用，1000 副本 → 1 个 ICQ                                │
│                                                                               │
│  5. Plugin + Master 协作                                                      │
│     ├─ Plugin 首次计算 status (消除异步等待)                                  │
│     ├─ Master 后续响应式更新                                                  │
│     ├─ status.conditions[Ready] 协调 writer                                  │
│     └─ 理由: 首次无等待 + 后续自动更新                                        │
│                                                                               │
│  6. 漂移检测由 nfd-master 负责                                                │
│     ├─ 对比新旧 status.compatibleNodes                                        │
│     ├─ 生成 NodeCompatibilityDrift Event                                     │
│     ├─ 可配置 postDriftPolicy (ignore/taint/deschedule)                      │
│     └─ 理由: nfd-master 是权威数据源，不影响调度热路径                        │
│                                                                               │
│  7. 降级机制                                                                  │
│     ├─ Webhook 故障 → Scheduler 同步解析 + 创建 ICQ                          │
│     ├─ Registry 不可达 → failurePolicy: Ignore/Fail                          │
│     └─ 理由: 保证功能可用性，代价是首次延迟增加                               │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```
