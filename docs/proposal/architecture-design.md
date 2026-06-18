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
│  │         │ 1.Create ICQ spec       │ 2.Update NFG status      │ 3.Compute │   │
│  │         │                         │                          │  ICQ stat │   │
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
│  │  │  │   (updated by       │              │   (computed by         │ │   │   │
│  │  │  │    nfd-master)      │              │    scheduler plugin)   │ │   │   │
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
│   │  │ Step 2: Check ICQ CR & Create if needed                          │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each image:                                                │  │ │
│   │  │     parse image → digest                                         │  │ │
│   │  │     query K8s API: ICQ `icq-{digest}` exists?                   │  │ │
│   │  │       exists → reuse (no creation needed)                        │  │ │
│   │  │       not exists → fetch OCI Artifact → parse → create ICQ CR   │  │ │
│   │  │                                                                   │  │ │
│   │  │   ICQ CR = persistent cache (stored in etcd)                    │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Step 3: Annotate Pod & Admit                                     │  │ │
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
│                    Phase 2: NFD 特征收集与 NodeFeatureGroup 更新             │
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
│   │  │ Update NodeFeatureGroup (admin pre-group)                        │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each pre-group:                                            │  │ │
│   │  │     collect node features                                        │  │ │
│   │  │     check homogeneity (internal, no exposed field)               │  │ │
│   │  │     update status.nodes                                          │  │ │
│   │  │                                                                   │  │ │
│   │  │   Note: nfd-master does NOT update ICQ status                    │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   └─────────────────────────────────────────────────────────────────────────┘ │
│                              │                                                 │
│                              │ Update NodeFeatureGroup status                 │
│                              ▼                                                 │
│   ┌─────────┐                                                                  │
│   │ etcd    │ ← NodeFeatureGroup status.nodes updated                          │
│   └─────────┘                                                                  │
│                                                                                │
└──────────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────┐
│                    Phase 3: Scheduler 计算 ICQ Status                        │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│   ┌─────────────────────────────────────────────────────────────────────────┐ │
│   │                         Scheduler Plugin                                 │ │
│   │                                                                          │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ ICQ Status Computation (首次或节点变化时)                         │  │ │
│   │  │                                                                   │  │ │
│   │  │   for each ICQ:                                                  │  │ │
│   │  │     ┌────────────────────────────────────────────────────────┐  │  │ │
│   │  │     │ Compute status.compatibleNodes                          │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ compatibleNodes = []                                     │  │  │ │
│   │  │     │                                                          │  │  │ │
│   │  │     │ // Use pre-group acceleration                           │  │  │ │
│   │  │     │ for each NodeFeatureGroup (pre-group):                  │  │  │ │
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
│   │  │     │ status.conditions[Ready] = True                         │  │  │ │
│   │  │     └────────────────────────────────────────────────────────┘  │  │ │
│   │  └──────────────────────────────────────────────────────────────────┘  │ │
│   │                              │                                         │ │
│   │                              ▼                                         │ │
│   │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│   │  │ Drift Detection (节点变化时)                                      │  │ │
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
│   │  │   3. Affinity/NodeSelector compatibility:                         │  │ │
│   │  │      - Compatibility filtering produces compatibleNodes set       │  │ │
│   │  │      - Native K8s plugins then apply affinity/nodeSelector rules  │  │ │
│   │  │      - Final nodes must satisfy BOTH constraints (intersection)   │  │ │
│   │  │      - Example: compatibility=[1..500], affinity=[300..800]       │  │ │
│   │  │                → final candidates = [300..500]                    │  │ │
│   │  │                                                                   │  │ │
│   │  │   4. Return filtered nodes to scheduler framework                 │  │ │
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
│    compatibilityRules:                                                       │
│      - name: "image-compatibility"                                           │
│        matchFeatures:                                                        │
│          - feature: kernel.version                                           │
│            matchExpressions:                                                 │
│              major: {op: In, value: ["6"]}                                   │
│          - feature: cpu.cpuid                                                │
│            matchExpressions:                                                 │
│              AVX2: {op: Is, value: true}                                     │
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
│  创建者: Webhook (spec only)                                                   │
│  更新者: Scheduler Plugin (status.compatibleNodes)                             │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 性能优化策略

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          性能优化层次                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  Layer 1: ICQ CR 持久化缓存                                                 │
│  ├─ ICQ CR 持久化在 etcd 中 (按 image digest 命名)                          │
│  ├─ 1000 副本 Deployment → 仅 1 次 registry 拉取                            │
│  ├─ 多副本 webhook 共享同一个 etcd 中的 ICQ                                 │
│  └─ 延迟: ICQ 已存在 ~ms 级，不存在 ~100-500ms                              │
│                                                                               │
│  Layer 2: Image Digest 去重                                                   │
│  ├─ ICQ 按 image digest 命名 (icq-sha256-{prefix})                          │
│  ├─ 相同镜像复用同一个 ICQ                                                    │
│  ├─ Refcount 跟踪引用数                                                      │
│  └─ 1000 副本 → 1 个 ICQ CR                                                 │
│                                                                               │
│  Layer 3: 预分组加速 (Scheduler Plugin 侧)                                  │
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
│                          漂移处理两层设计                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │ 场景 1: 调度前漂移 (Pod 还没调度)                                     │  │
│  │                                                                        │  │
│  │  第一层: ICQ status 异步更新 (覆盖 99% 场景)                          │  │
│  │    节点漂移 → NodeFeature 更新 → informer 回调                        │  │
│  │    → Scheduler Plugin 重算 ICQ status.compatibleNodes                 │  │
│  │    → 漂移节点从 compatibleNodes 中移除                                │  │
│  │    → 后续调度的 Pod 自动避开漂移节点 ✓                                │  │
│  │                                                                        │  │
│  │  第二层: PreBind 实时验证 (兜底 1% 竞态场景)                          │  │
│  │    如果 informer 回调延迟，ICQ status 过时:                           │  │
│  │    → PreBind 用节点最新特征做实时验证                                 │  │
│  │    → 发现漂移 → 拒绝绑定 → 重新调度 ✓                               │  │
│  │                                                                        │  │
│  │  结果: 新 Pod 不会调度到漂移节点                                      │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │ 场景 2: 调度后漂移 (Pod 已在运行)                                     │  │
│  │                                                                        │  │
│  │  节点漂移 → NodeFeature 更新 → informer 回调                          │  │
│  │  → Scheduler Plugin 重算 ICQ status.compatibleNodes                   │  │
│  │  → 检测受影响的 Pod (运行在漂移节点上，且镜像匹配该 ICQ)              │  │
│  │                                                                        │  │
│  │  对每个受影响的 Pod:                                                  │  │
│  │    1. 给 Pod 打 label:                                                │  │
│  │       nfd.k8s-sigs.io/compatibility-drift: "true"                     │  │
│  │       nfd.k8s-sigs.io/drift-node: "node-50"                           │  │
│  │       nfd.k8s-sigs.io/drift-time: "2026-06-15T10:30:00Z"             │  │
│  │                                                                        │  │
│  │    2. 记录结构化日志:                                                 │  │
│  │       {                                                                │  │
│  │         "level": "warn",                                              │  │
│  │         "event": "NodeCompatibilityDrift",                            │  │
│  │         "pod": "my-app-abc123",                                       │  │
│  │         "node": "node-50",                                            │  │
│  │         "image": "registry.example.com/app@sha256:aaa...",            │  │
│  │         "drifted_features": [...]                                     │  │
│  │       }                                                                │  │
│  │                                                                        │  │
│  │    3. 生成 K8s Event (可选):                                          │  │
│  │       type: Warning                                                   │  │
│  │       reason: NodeCompatibilityDrift                                  │  │
│  │       message: "Node drifted, pod running on incompatible node"       │  │
│  │                                                                        │  │
│  │  结果: 管理员通过 label/log/event 发现漂移，决定是否迁移              │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 调度前漂移：两层防护

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  调度前漂移处理流程                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  T1: node-50 内核升级，特征漂移                                              │
│  T2: NFD worker 上报新特征                                                   │
│  T3: nfd-master 更新 NodeFeature                                             │
│                                                                               │
│  ─────────────────────────────────────────────────────────────────────────  │
│  第一层: ICQ status 异步更新 (覆盖 99% 场景)                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                                                                               │
│  T4: Scheduler Plugin informer 回调触发                                      │
│  T5: 重算 ICQ status.compatibleNodes                                         │
│      → node-50 从 compatibleNodes 中移除                                     │
│                                                                               │
│  T6: 新 Pod 进入调度队列                                                     │
│  T7: Prefilter 读取 ICQ status                                               │
│      → compatibleNodes = [node-1..node-49, node-51..node-100]               │
│      → node-50 不在列表中 ✓                                                 │
│  T8: Filter 过滤候选节点                                                     │
│  T9: Score 选择最优节点                                                      │
│  T10: PreBind 验证 (此时 ICQ status 已是最新)                                │
│      → 验证通过 ✓                                                           │
│  T11: Bind 绑定 Pod 到节点                                                   │
│                                                                               │
│  结果: Pod 调度到兼容节点 ✓                                                 │
│                                                                               │
│  ─────────────────────────────────────────────────────────────────────────  │
│  第二层: PreBind 实时验证 (兜底 1% 竞态场景)                                │
│  ─────────────────────────────────────────────────────────────────────────  │
│                                                                               │
│  T4': 新 Pod 进入调度队列 (informer 回调还未触发)                            │
│  T5': Prefilter 读取 ICQ status                                              │
│      → compatibleNodes = [node-1..node-50..node-100] (过时)                 │
│      → node-50 还在列表中 ✗                                                 │
│  T6': Filter 过滤候选节点                                                    │
│  T7': Score 选择最优节点 → node-50                                           │
│                                                                               │
│  T8': PreBind 实时验证 (关键!)                                               │
│      → 从 informer 读取 node-50 的最新特征                                   │
│      → 用最新特征 vs ICQ 兼容性规则做匹配                                    │
│      → 匹配失败! node-50 已漂移                                              │
│      → 拒绝绑定，返回 Unschedulable                                          │
│      → 生成 Event: "Node drifted during scheduling"                         │
│                                                                               │
│  T9': 调度框架重新调度 Pod                                                   │
│  T10': 下次调度时，informer 回调已触发，ICQ status 已更新                    │
│      → Pod 调度到兼容节点 ✓                                                 │
│                                                                               │
│  结果: PreBind 拦截漂移节点，保证调度正确性 ✓                               │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### PreBind 验证逻辑

```go
func (p *Plugin) PreBind(ctx context.Context, state *framework.CycleState, 
                         pod *v1.Pod, nodeName string) *framework.Status {
    // 1. 获取 Pod 的所有镜像 digest
    digests := getImageDigests(pod)
    
    // 2. 获取选中节点的最新特征 (从 informer 缓存，接近实时)
    nodeFeature := p.nodeFeatureLister.Get(nodeName)
    if nodeFeature == nil {
        return framework.NewStatus(framework.Error, "NodeFeature unavailable")
    }
    
    // 3. 对每个镜像做实时兼容性验证
    for _, digest := range digests {
        icq := p.icqLister.Get("icq-" + digest)
        if icq == nil {
            continue // ICQ 不存在，跳过
        }
        
        // 4. 实时匹配：用节点最新特征 vs ICQ 兼容性规则
        if !p.matcher.Match(nodeFeature, icq.Spec.CompatibilityRules) {
            // 5. 不兼容！拒绝绑定
            p.recordEvent(pod, v1.EventTypeWarning, "NodeCompatibilityDrift",
                fmt.Sprintf("Node %s drifted during scheduling", nodeName))
            
            return framework.NewStatus(framework.Unschedulable, 
                "Node compatibility check failed in PreBind")
        }
    }
    
    // 6. 全部通过，允许绑定
    return framework.NewStatus(framework.Success, "")
}
```

### 调度后漂移：label + log 告警

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  调度后漂移处理流程                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  T1: Pod 已调度到 node-50，正常运行                                          │
│  T2: node-50 内核升级，特征漂移                                              │
│  T3: NFD worker 上报新特征                                                   │
│  T4: nfd-master 更新 NodeFeature                                             │
│                                                                               │
│  T5: Scheduler Plugin informer 回调触发                                      │
│  T6: 重算 ICQ status.compatibleNodes                                         │
│      → node-50 从 compatibleNodes 中移除                                     │
│                                                                               │
│  T7: 检测受影响的 Pod                                                        │
│      → 查找运行在 node-50 上的所有 Pod                                       │
│      → 对每个 Pod，检查其镜像是否匹配该 ICQ                                  │
│      → 找到受影响的 Pod: [pod-A, pod-B, pod-C]                              │
│                                                                               │
│  T8: 对每个受影响的 Pod，执行告警:                                           │
│                                                                               │
│      ┌────────────────────────────────────────────────────────────────┐     │
│      │ 1. 给 Pod 打 label                                             │     │
│      │    kubectl label pod pod-A \                                   │     │
│      │      nfd.k8s-sigs.io/compatibility-drift=true \                │     │
│      │      nfd.k8s-sigs.io/drift-node=node-50 \                      │     │
│      │      nfd.k8s-sigs.io/drift-time=2026-06-15T10:30:00Z           │     │
│      │                                                                 │     │
│      │    作用:                                                        │     │
│      │    - 可通过 label selector 查询所有受漂移影响的 Pod             │     │
│      │    - 可被监控系统采集                                           │     │
│      │    - 可用于自动化脚本 (如自动迁移)                              │     │
│      └────────────────────────────────────────────────────────────────┘     │
│                                                                               │
│      ┌────────────────────────────────────────────────────────────────┐     │
│      │ 2. 记录结构化日志                                              │     │
│      │    {                                                            │     │
│      │      "timestamp": "2026-06-15T10:30:00Z",                      │     │
│      │      "level": "warn",                                          │     │
│      │      "event": "NodeCompatibilityDrift",                        │     │
│      │      "pod": {                                                  │     │
│      │        "name": "pod-A",                                        │     │
│      │        "namespace": "production",                              │     │
│      │        "uid": "pod-uid-xxx"                                    │     │
│      │      },                                                        │     │
│      │      "node": "node-50",                                        │     │
│      │      "image": {                                                │     │
│      │        "ref": "registry.example.com/app@sha256:aaa...",        │     │
│      │        "digest": "sha256:aaa..."                               │     │
│      │      },                                                        │     │
│      │      "drifted_features": [                                     │     │
│      │        {                                                       │     │
│      │          "feature": "kernel.version",                          │     │
│      │          "old_value": "6.8",                                   │     │
│      │          "new_value": "6.9",                                   │     │
│      │          "rule": "major In [\"6\"]"                            │     │
│      │        }                                                       │     │
│      │      ]                                                         │     │
│      │    }                                                            │     │
│      │                                                                 │     │
│      │    作用:                                                        │     │
│      │    - 可被日志平台 (ELK/Loki) 索引和查询                        │     │
│      │    - 包含详细的漂移特征信息，便于排查                           │     │
│      │    - 可触发告警规则                                             │     │
│      └────────────────────────────────────────────────────────────────┘     │
│                                                                               │
│      ┌────────────────────────────────────────────────────────────────┐     │
│      │ 3. 生成 K8s Event (可选)                                       │     │
│      │    apiVersion: v1                                              │     │
│      │    kind: Event                                                 │     │
│      │    type: Warning                                               │     │
│      │    reason: NodeCompatibilityDrift                              │     │
│      │    regarding:                                                  │     │
│      │      kind: Pod                                                 │     │
│      │      name: pod-A                                               │     │
│      │      namespace: production                                     │     │
│      │    note: |                                                     │     │
│      │      Node "node-50" no longer satisfies compatibility          │     │
│      │      requirements for image "registry.example.com/app...".     │     │
│      │      Drifted features:                                         │     │
│      │        - kernel.version: 6.8 → 6.9                             │     │
│      │      Action: Run `kubectl drain node-50` to migrate.           │     │
│      │                                                                 │     │
│      │    作用:                                                        │     │
│      │    - kubectl describe pod 可直接看到告警                        │     │
│      │    - 可被 Event 聚合工具收集                                    │     │
│      │    - 与 K8s 原生事件系统一致                                    │     │
│      └────────────────────────────────────────────────────────────────┘     │
│                                                                               │
│  T9: 管理员收到告警，决定处理方式:                                           │
│      ─────────────────────────────────────────────────────────────────────  │
│      选项 A: 忽略                                                            │
│        - Pod 继续运行                                                        │
│        - 等待下次自然迁移 (如 Deployment 滚动更新)                           │
│      ─────────────────────────────────────────────────────────────────────  │
│      选项 B: 手动迁移                                                        │
│        - kubectl drain node-50                                               │
│        - Pod 重新调度到兼容节点                                              │
│        - 迁移后移除 label:                                                   │
│          kubectl label pod pod-A nfd.k8s-sigs.io/compatibility-drift-        │
│      ─────────────────────────────────────────────────────────────────────  │
│      选项 C: 修复节点                                                        │
│        - 恢复节点的兼容性特征                                                │
│        - 节点重新进入 compatibleNodes                                        │
│        - 移除 Pod 的 drift label                                             │
│      ─────────────────────────────────────────────────────────────────────  │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 用户查询漂移 Pod

```bash
# 查询所有受漂移影响的 Pod
kubectl get pods --all-namespaces -l nfd.k8s-sigs.io/compatibility-drift=true

# 查询特定节点的漂移 Pod
kubectl get pods --all-namespaces -l nfd.k8s-sigs.io/drift-node=node-50

# 查看 Pod 的漂移详情
kubectl describe pod pod-A -n production
# Events:
#   Warning  NodeCompatibilityDrift  1h    nfd-image-compat-scheduler
#     Node "node-50" no longer satisfies compatibility requirements...

# 批量迁移漂移 Pod
kubectl get pods --all-namespaces -l nfd.k8s-sigs.io/compatibility-drift=true \
  -o jsonpath='{range .items[*]}kubectl drain {.spec.nodeName} --ignore-daemonsets --delete-emptydir-data{"\n"}{end}'

# 迁移后移除 label
kubectl label pod pod-A -n production nfd.k8s-sigs.io/compatibility-drift-
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
│  │ Webhook         │ • Fetch OCI Artifact (if ICQ not exists)              │ │
│  │                 │ • Parse compatibility metadata                        │ │
│  │                 │ • Create ImageCompatibilityQuery CR (spec only)       │ │
│  │                 │ • Annotate Pod with image digests                     │ │
│  │                 │ • Check ICQ existence before creation (reuse)         │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ nfd-master      │ • Collect NodeFeature from NFD workers                │ │
│  │                 │ • Update NodeFeatureGroup status.nodes                │ │
│  │                 │   (admin pre-group only, NOT ICQ)                     │ │
│  │                 │ • Internal homogeneity detection for pre-groups       │ │
│  ├─────────────────┼──────────────────────────────────────────────────────┤ │
│  │ Scheduler       │ • Read Pod annotations (image digests)                │ │
│  │ Plugin          │ • Query ICQ from informer cache                       │ │
│  │                 │ • Prefilter:                                          │ │
│  │                 │   - Check ICQ existence                               │ │
│  │                 │   - Fallback: sync OCI fetch + ICQ creation           │ │
│  │                 │   - Compute & write ICQ status.compatibleNodes        │ │
│  │                 │   - Store compatibleNodes in SchedulingContext        │ │
│  │                 │ • Filter:                                             │ │
│  │                 │   - Compute intersection of all ICQ compatibleNodes   │ │
│  │                 │   - Filter candidate nodes                            │ │
│  │                 │ • PreBind:                                            │ │
│  │                 │   - Real-time compatibility verification              │ │
│  │                 │   - Reject bind if node drifted during scheduling     │ │
│  │                 │ • Update ICQ refcount                                 │ │
│  │                 │ • Watch NodeFeature changes via informer              │ │
│  │                 │   - Recompute ICQ status when nodes change            │ │
│  │                 │   - Detect drift (compare old/new status)             │ │
│  │                 │   - Label affected Pods with drift info               │ │
│  │                 │   - Log drift events (structured JSON)                │ │
│  │                 │   - Generate K8s Events (optional)                    │ │
│  │                 │ • GC: Monitor refcount, delete ICQ when expired       │ │
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
│  3. Webhook 预解析与本地缓存                                                  │
│     ├─ Mutating Webhook + 本地 LRU 缓存 + ICQ CR 持久化                      │
│     ├─ 本地缓存: LRU + TTL 60s，避免短时间内重复访问 Registry                │
│     ├─ ICQ CR: 持久化缓存，多实例共享                                        │
│     ├─ 组合 Key: icq-{image-digest}-{artifact-digest}                        │
│     └─ 理由: 避免 Registry 限流 + 自然处理 artifact 变化                     │
│                                                                               │
│  4. Image Digest 去重                                                         │
│     ├─ ICQ 按 image digest 命名                                              │
│     ├─ Refcount 跟踪引用数                                                   │
│     └─ 理由: 最大化复用，1000 副本 → 1 个 ICQ                                │
│                                                                               │
│  5. Scheduler Plugin 管理 ICQ 生命周期                                        │
│     ├─ Webhook 创建 ICQ spec (仅 spec)                                        │
│     ├─ Scheduler plugin 计算并更新 status.compatibleNodes                     │
│     ├─ Scheduler plugin 监听 NodeFeature 变化，主动重新计算 ICQ status        │
│     ├─ Scheduler plugin 管理 refcount 和 GC                                   │
│     └─ 理由: ICQ 是调度相关资源，由调度组件管理，职责边界清晰                   │
│                                                                               │
│  6. 漂移处理两层设计                                                          │
│     ├─ 调度前漂移:                                                            │
│     │   ├─ 第一层: ICQ status 异步更新 (覆盖 99% 场景)                       │
│     │   ├─ 第二层: PreBind 实时验证 (兜底 1% 竞态场景)                       │
│     │   └─ 结果: 新 Pod 不会调度到漂移节点                                   │
│     ├─ 调度后漂移:                                                            │
│     │   ├─ 给 Pod 打 label (nfd.k8s-sigs.io/compatibility-drift)             │
│     │   ├─ 记录结构化日志 (JSON 格式，包含漂移特征详情)                      │
│     │   ├─ 生成 K8s Event (可选)                                             │
│     │   └─ 结果: 管理员通过 label/log/event 发现漂移，决定是否迁移           │
│     └─ 理由:                                                                  │
│         ├─ PreBind 开销极小 (< 1ms)，保证调度正确性                          │
│         ├─ 不做自动迁移，避免侵入性操作                                      │
│         ├─ label 便于查询和自动化脚本                                         │
│         └─ 结构化日志便于日志平台索引和告警                                   │
│                                                                               │
│  7. 降级机制                                                                  │
│     ├─ Webhook 故障 → Scheduler 同步解析 + 创建 ICQ                          │
│     ├─ Registry 不可达 → failurePolicy: Ignore/Fail                          │
│     └─ 理由: 保证功能可用性，代价是首次延迟增加                               │
│                                                                               │
│  8. Affinity/NodeSelector 兼容性                                              │
│     ├─ 兼容性过滤在 Filter 阶段执行，产生 compatibleNodes 集合               │
│     ├─ 原生 K8s 调度器插件随后应用 affinity/nodeSelector 规则                │
│     ├─ 最终节点必须同时满足两个约束 (取交集)                                 │
│     ├─ 示例: compatibility=[1..500], affinity=[300..800]                     │
│     │        → final candidates = [300..500]                                 │
│     └─ 理由: 与现有 Pod spec 向后兼容，允许用户组合兼容性需求和其他调度约束  │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Webhook 本地缓存与 Artifact 变化处理

### 设计思路

本地缓存（LRU + TTL 60s）和 artifact 变化处理是**同一个流程的不同阶段**，不需要额外的后台 controller。

### 缓存架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        Webhook 缓存层次                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Layer 1: 进程内 LRU 缓存                                       │
│  ├─ Key: image-digest                                           │
│  ├─ Value: {artifact-digest, ICQ spec, timestamp}               │
│  ├─ TTL: 60 秒                                                  │
│  ├─ 容量: 10000 条目                                            │
│  └─ 作用: 避免短时间内重复访问 Registry                          │
│                                                                  │
│  Layer 2: ICQ CR (Kubernetes etcd)                              │
│  ├─ Key: icq-{image-digest}-{artifact-digest}                   │
│  ├─ Value: ICQ CR spec + status                                 │
│  └─ 作用: 持久化缓存，多实例共享                                │
│                                                                  │
│  Layer 3: Registry                                              │
│  └─ 作用: 最终数据源                                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 完整工作流程

```
Pod CREATE 请求
    │
    ▼
对于每个容器镜像:
    │
    ├─ Step 1: 查本地缓存
    │   key = image-digest
    │   ├─ 命中且未过期 (<60s) → 直接使用缓存数据
    │   └─ 未命中或过期 → 继续
    │
    ├─ Step 2: 访问 Registry
    │   ├─ 获取 image manifest → image-digest
    │   ├─ 获取 OCI artifact manifest → artifact-digest
    │   └─ 解析兼容性元数据 → ICQ spec
    │
    ├─ Step 3: 检查 ICQ CR 是否存在
    │   key = icq-{image-digest}-{artifact-digest}
    │   ├─ 存在 → 加载到本地缓存
    │   └─ 不存在 → 创建 ICQ CR，加载到本地缓存
    │
    └─ Step 4: 在 Pod annotation 中记录 ICQ 引用
        └─ nfd.k8s-sigs.io/icq-refs: "icq-xxx,icq-yyy"
```

### 场景演示：Artifact 变化处理

```
T0: 镜像 v1 发布
    - image-digest = "sha256:aaa"
    - artifact-digest = "sha256:xxx"
    - 兼容性要求: kernel>=5

T1: Pod-1 创建
    - 本地缓存 miss
    - 访问 Registry → artifact-digest = "sha256:xxx"
    - 构造 ICQ 名称: icq-sha256-aaa-sha256-xxx
    - ICQ 不存在 → 创建 ICQ (spec: {kernel>=5})
    - 更新本地缓存: {sha256:aaa → {artifact:xxx, spec:{kernel>=5}}, ttl=60s}

T2~T59: Pod-2 ~ Pod-59 创建
    - 本地缓存 hit (<60s)
    - 直接使用缓存数据
    - 0 次 Registry 请求

T60: TTL 过期

T61: Pod-60 创建
    - 本地缓存过期
    - 访问 Registry
    - 发现 artifact-digest 已变为 "sha256:yyy"（镜像作者更新了兼容性元数据）
    - 构造 ICQ 名称: icq-sha256-aaa-sha256-yyy（不同！）
    - ICQ 不存在 → 创建新 ICQ（新规则: kernel>=6）
    - 更新本地缓存: {sha256:aaa → {artifact:yyy, spec:{kernel>=6}}, ttl=60s}

T62~T120: Pod-61 ~ Pod-120 创建
    - 本地缓存 hit (<60s)
    - 使用新 ICQ (kernel>=6)

T120: TTL 过期，再次访问 Registry
    - artifact-digest 未变 → 复用现有 ICQ
```

### 旧 ICQ 的清理

```
T120: 旧 ICQ (icq-sha256-aaa-sha256-xxx) 的引用情况
    - 所有 Pod 都已使用新 ICQ
    - 旧 ICQ refcount = 0
    - 启动 TTL 清理计时器（默认 1 小时）

T120 + 1h: 旧 ICQ 被删除
```

### 避免 Registry 限流

| 场景 | 无缓存 | 有 LRU + TTL |
|------|--------|-------------|
| 1000 Pod，相同镜像 | 1000 次 registry 请求 | 1 次 registry 请求（首次） |
| 1000 Pod，10 个镜像 | 1000 次请求 | 10 次请求（首次） |
| 后续请求（60 秒内） | 每次都访问 | 缓存命中，0 次请求 |

### 关键设计决策

| 机制 | 作用 | 时间尺度 |
|------|------|---------|
| LRU 本地缓存 | 避免短时间内重复访问 Registry | 60 秒 |
| 组合 Key | 确保 artifact 更新时创建新 ICQ | 即时 |
| ICQ refcount | 跟踪哪些 Pod 使用哪个 ICQ | Pod 生命周期 |
| ICQ TTL 清理 | 清理 refcount=0 的旧 ICQ | 1 小时 |

### 为什么不需要后台 Controller？

```
传统方案:
  - 后台 controller 定期检查所有 ICQ
  - 发现 artifact 变化 → 更新 ICQ
  - 问题: 需要额外的 controller，增加复杂性

当前方案:
  - TTL 过期后自然访问 Registry
  - 发现 artifact 变化 → 创建新 ICQ
  - 优势: 无需额外 controller，按需更新
```

### 总结

**本地缓存和 artifact 变化处理是同一个流程**：
1. **短期（<60s）**：本地缓存避免重复访问 Registry → 避免限流
2. **长期（>60s）**：TTL 过期后自然发现 artifact 变化 → 创建新 ICQ
3. **清理**：旧 ICQ 通过 refcount + TTL 自动清理

**无需额外组件**：不需要后台 controller，所有逻辑都在 webhook 的请求处理流程中完成。
