# KEP-2403 Review 意见与解决方案

## PR 概览

- **PR**: [kubernetes-sigs/node-feature-discovery#2403](https://github.com/kubernetes-sigs/node-feature-discovery/pull/2403)
- **标题**: Add NFD image compatibility scheduler proposal
- **作者**: Xunli-Yang, ChaoyiHuang
- **状态**: Open (Changes Requested)
- **首选方案**: Proposal C (Node Pre-grouping / 节点预分组)

---

## 1. ArangoGutierrez (Maintainer) — 2026年1月15日

### 倾向方向: 方案 3 (节点预分组)

理由:
1. 符合真实集群管理实践 — 运维人员已经在按硬件特征组织节点池/分组
2. O(G) vs O(N) 在大规模场景下至关重要 — 10 个代表节点 vs 10,000 个节点
3. 架构更简单 — 不需要引入 SQL 数据库
4. 渐进路径 — 可以先用方案 1 作为 MVP，再叠加方案 3 的优化

### 问题与解答

| # | 问题 | 作者回复 | 状态 |
|---|------|---------|------|
| 1 | **调度插件归属**: 是否应该放在 `kubernetes-sigs/scheduler-plugins`？ | 目标: 集成到 scheduler-plugins；初期在 NFD SIG 孵化。后在 Amsterdam KubeCon 确认: 放在 NFD repo。 | 已解决 |
| 2 | **NFG 生命周期管理**: 临时 CR 的清理策略？ | 调度器创建的临时 CR 将通过 TTL 进行垃圾回收。后细化为: 基于 Spec 的 hash 去重 + 引用计数 + 可配置 TTL。 | 已解决 |
| 3 | **分组同构性保障**: 如何验证/强制同构性？特征漂移怎么办？ | 管理员负责初始同构性。NFG 更新处理漂移。需要监控机制来检测漂移节点。 | 部分解决 — 需要验证机制 |
| 4 | **Prefilter 阶段 OCI artifact 拉取失败** | 降级策略: 继续调度并记录 warning（默认 fail-open）。后增加可配置的 `defaultCompatibilityFailurePolicy`: Ignore（默认）或 Fail。 | 已解决 |
| 5 | **NFG status 过期或 controller 更新缓慢** | 重试调度；考虑在 binding 前做最后一秒验证。通过 Informer/Watch + MovePodToActiveQueue 实现调度器 requeue。 | 已解决 |
| 6 | **没有匹配的分组** | 调度重试直到失败，记录 error 日志。 | 已解决 |

### 缺失的 KEP 章节

| 章节 | 状态 |
|------|------|
| Risks and Mitigations（风险与缓解） | 已添加 |
| Graduation Criteria（毕业标准 Alpha/Beta/GA） | 已添加 |
| Implementation Timeline / Milestones（实施时间线/里程碑） | 部分添加 |
| Alternatives Considered（备选方案） | 已添加 |
| Goals / Non-Goals（目标/非目标） | 已添加（5月22日 commit） |

---

## 2. ChaoyiHuang (Co-author / 联合作者) — 2026年1月20日 & 23日

### 意见与解决方案

| # | 意见 | 解决方案 | 状态 |
|---|------|---------|------|
| 1 | **插件归属**: `scheduler-plugins` 是 out-of-tree 的；不保证各发行版会包含。NFD 已经被几乎所有发行版集成。 | 在 Amsterdam KubeCon 确认（2026年4月14日）: 放在 NFD repo。原因: (1) in-tree 需要改核心二进制 + 跟随主版本发布周期；(2) scheduler-plugins out-of-tree 与 NFD 没有发布周期保证；(3) NFD 已在大多数发行版中。 | 已解决 |
| 2 | **NFG 复用**: 多个 Pod 可能复用同一镜像。建议可配置的 purge 策略（如 1h/24h TTL）以便 CR 复用。 | 基于 Spec 的 hash 去重: 临时 NFG 以兼容性需求规格的 hash 值命名。Annotation 中记录引用计数。仅当 refcount=0 且超过空闲超时（默认 1h）后才触发 GC。 | 已解决 |
| 3 | **更智能的降级**（5月22日）: 当 Homogeneous=False 时，区分漂移的特征是否影响兼容性过滤。 | 值得深入研究。两种情况: (1) 漂移的特征不影响兼容性过滤 → 无需降级；(2) 漂移的特征影响兼容性过滤 → 降级到逐节点扫描。 | 待定 — 需要设计 |

---

## 3. colvert (Reviewer) — 2026年5月11日

### 意见与解决方案

| # | 意见 | 解决方案 | 状态 |
|---|------|---------|------|
| 1 | **Summary 文本**: 丰富背景描述，提及工作负载适配复杂性和部署延迟。 | 背景描述已在5月18日 commit 中丰富。 | 已解决 |
| 2 | **方案 C vs B**: 需要说明为什么选 C 而非 B。B 允许预检查但需要拓扑管理。 | B 引入 SQLite + PVC 到 NFD master — 基础设施改动大，超出本 KEP 范围。C 利用现有 NFG 机制，复杂度在分组策略而非存储后端。 | 已解决 |
| 3 | **预分组组合数**: 即使有模板，组合数仍可能很高。 | 分组应使用关键兼容性维度（如 CPU 架构 + 内核版本），不需要覆盖所有特征组合。 | 部分解决 |
| 4 | **预分组失败**: 分组声明可能过于严格 — "分组的声明将是关键"。 | 已确认。需要为管理员提供分组策略指导。 | 部分解决 |
| 5 | **Node affinity 与 NFD scheduler 的交互**: 当工作负载供应商设置了 affinity/nodeSelector 时，NFD scheduler 如何行为？ | 兼容性过滤在 Filter 阶段先执行；affinity/nodeSelector 由 Kubernetes 原生插件后续处理。两者是 AND 关系。 | 已解决 |

---

## 4. ArangoGutierrez — 第二次详细 Review（2026年5月11日）

### LGTM 前需要解决的 6 个主题

| # | 主题 | 详情 | 解决方案 | 状态 |
|---|------|------|---------|------|
| 1 | **分组同构性验证** | 代表节点采样是正确性的核心支撑，但当前完全委托给管理员，没有验证机制。 | nfd-master 中基于 hash 的同构性验证；`Status.Conditions.Homogeneous=True/False`。Homogeneous 是 NFG status 计算逻辑的内在部分（True → 代表节点匹配 O(1)，False → 逐节点匹配 O(N)），不是 scheduler 的独立校验步骤。scheduler 直接信任 NFG status.nodes。详见第 8 节。 | 已达成共识 — 需更新文档 |
| 2 | **Prefilter 中的 OCI 拉取** | Registry I/O 在调度热路径上，会触发 rate limit，且忽略 `imagePullSecrets`。 | 引入 pre-scheduler controller 或 admission webhook，按 image digest 预解析兼容性并缓存。参考: sigstore policy-controller, Kyverno verify-images。 | 已达成共识 — 待完成 |
| 3 | **NFG Kind 过载** | 管理员定义的长期 NFG 和调度器创建的临时 NFG 共享一个 Kind，但生命周期/写入者/RBAC 不同。 | 分离 Kind（如 `NodeCompatibilityQuery`），scheduler 拥有，nfd-master 忽略。参考: Karpenter 的 NodePool vs NodeClaim。 | 已达成共识 — 需进一步分析 |
| 4 | **同步与写放大** | Prefilter 创建 NFG → Filter 读 `.status`，但 status 更新经过 nfd-master 的 rate-limited workqueue（`rate.Limit(10)`, bucket 100）。 | 如果评估移到 plugin 侧执行，则绕过此瓶颈。否则需要 spec 化 trigger field + 节点粒度更新 API。按 spec hash 去重，使 1000 副本的 Deployment 只创建一个 CR。 | 已达成共识 — 需要架构决策 |
| 5 | **标准 KEP 章节** | Goals/Non-Goals、feature gate 名称、metrics、带数字的可扩展性目标、rollback 方案。 | Goals/Non-Goals 已添加。Feature gate、metrics、rollback 仍需要。 | 部分解决 |
| 6 | **归属决策** | Amsterdam sig-scheduling 讨论仅存在于 PR 评论中 — 应写入文档。 | 需要添加归属决策章节并附 meeting notes 链接。需要 OWNERS 中指定一位 sig-scheduling reviewer。 | 待定 |

---

## 5. ArangoGutierrez — 第三次 Review（2026年6月11日）

### 总体评估

提案现在读起来是一个连贯的 Phase-2 方向。大多数主要关注点已有共识性解决方案。剩余工作是将 checklist 中的内容落实到文档正文中。

### 新增待解决问题

| # | 问题 | 详情 | 建议方案 | 状态 |
|---|------|------|---------|------|
| 1 | **评估归属** | 步骤 2 有歧义 — 是 plugin 执行代表节点匹配并写入 NFG status，还是 nfd-master 做？这是两种不同架构: plugin 侧需要 NodeFeature informers + NFD matcher 库；master 侧需要节点粒度 NFG 更新 API 和 spec 化的 trigger。 | 采用方案 C（Plugin + Master 协作）: 首次创建由 Plugin 本地计算并写入 status（绕过 nfd-master rate-limit 瓶颈），后续节点特征变化由 Master 响应式更新。两者共享 NFD matcher 库，通过 `status-initialized` annotation 协调 writer。详见第 8 节。 | 已解决 |
| 2 | **多容器 Pod 组合** | Pod 有多个镜像（app + init + sidecar）。per-image-digest NFG（Filter 取交集）还是 per-pod NFG？这决定了 spec-hash key 和 refcount 粒度。 | 采用 Image 粒度 NFG 方案: 每个 image digest 创建一个 NFG CR，多容器 Pod 在 Filter 阶段对各镜像 NFG 的 status.nodes 取交集。通过 refcount + TTL 管理生命周期。详见第 7 节。 | 已解决 |
| 3 | **未分组节点** | "不存在兼容节点" 的结论只有在每个节点都属于某个预分组时才成立。目前没有要求分组覆盖整个集群。 | 引入隐式 residual set（未分组节点 → 默认组，逐节点评估），或在文档中明确未分组节点被排除在兼容性调度之外。 | 待定 |
| 4 | **性能预算假设** | 预算是否假设 warm cache？冷启动 Prefilter 包含 registry RTT + NFG 创建 + 等待 rate-limited nfd-master 的 status — 50ms p99 在冷启动下不可达。 | 区分 warm/cold 路径指标。增加 pod-arrival-to-bind 端到端 p99。定义 "成功率" 的含义及其时间窗口。 | 待定 |

---

## 6. Xunli-Yang — 综合回复（2026年5月21日）

### 行动项 Checklist

| # | 事项 | 状态 |
|---|------|------|
| 1 | 将机制细节移至 Design Details 章节 | 已完成 |
| 2 | 添加 Goals 和 Non-Goals | 已完成 |
| 3 | 优化措辞 | 已完成 |
| 4 | 丰富背景介绍 | 已完成 |
| 5 | 添加 affinity/node selector 说明 | 已完成 |
| 6 | 整合调度器 Requeue 机制 | 已完成 |
| 7 | NFG Spec-based hash 去重 + 引用计数 + 延迟删除 | 已完成 |
| 8 | 预分组同构性验证机制 | 已完成 |
| 9 | OCI 拉取: 按 image digest 预解析，为调度器缓存 | 待完成 |
| 10 | NFG RBAC 与生命周期分离 | 需进一步分析 |
| 11 | 元数据获取失败的 fail-safe 策略 | 已完成 |
| 12 | 量化 Test Plan 和 Graduation Criteria | 已完成 |

### 已达成共识的关键技术决策

#### 同构性（Homogeneous）的本质

- **同构性不是一个独立的"调度时校验步骤"**，而是 NFG status 计算逻辑的内在部分
- `Homogeneous` condition 是**性能优化指示器**，不是正确性约束
- `Homogeneous=True` → 计算时用代表节点匹配（O(1)）
- `Homogeneous=False` → 计算时用逐节点匹配（O(N)）
- 无论 Homogeneous 是什么值，NFG status.nodes 的结果都是正确的

#### 调度前漂移处理
- 节点特征漂移 → NFD worker 上报 → nfd-master 更新 NodeFeature → 自动触发所有相关 NFG 的 status 重新计算
- nfd-master 重算 admin pre-group NFG: status.nodes + feature hash → 更新 Homogeneous condition
- nfd-master 重算 image compat NFG: 根据 Homogeneous 选择计算方式 → 更新 status.nodes
- **scheduler 在 Filter 阶段直接信任 NFG status.nodes，不做额外同构性校验**
- 之前文档中"若 Homogeneous=False，降级到方案 A"的描述不准确 — 这不是 scheduler 的降级行为，而是 NFG status 计算时的内在逻辑

#### 调度后漂移处理
- 调度后漂移和同构性是两个独立的问题
- 调度后漂移: Pod 已在运行，节点特征变了 → 需要监控 + 告警 + 可选迁移
- 处理策略（可配置）:
  - `ignore`（默认）: 不干预，Pod 继续运行。兼容性 ≠ 可用性，强制迁移可能比继续运行风险更大
  - `taint`: 给节点打 taint，阻止新 Pod 调度
  - `deschedule`: 触发 descheduler 迁移 Pod
- 检测方式: nfd-master 更新 NFG status 时，对比新旧 status.nodes，找出"被移除的节点"，检查这些节点上是否有使用相关镜像的 Pod，生成 Event

#### NFG 生命周期 — Image 粒度 NFG + refcount + TTL
- 每个 image digest 对应一个 NFG CR（详见第 7 节）
- Annotation 中记录引用计数（调度引用时递增，完成时递减）
- 可配置 TTL（`ephemeral-nfg-idle-timeout`，默认 1 小时）
- 仅当 refcount 降至零且超过 TTL 后才触发 GC
- Pod 异常退出导致 refcount 没递减 → TTL 兜底

#### 调度器 Requeue
- 若 NFG Status 未就绪 → 标记 Pod 为 unschedulable 并 back off
- 通过 Informer/Watch 监听 NFG 更新事件
- Status 填充后触发 `MovePodToActiveQueue`

#### 故障策略
- `defaultCompatibilityFailurePolicy` 参数:
  - `Ignore`（Fail-open）: 默认。记录 warning，继续调度
  - `Fail`（Fail-closed）: 标记 Pod 为 Unschedulable，直到 Registry 恢复

#### 同构性配置
- `NodeFeatureGroup.spec.homogeneityGuarantee: enforced` 开关
- 仅控制 nfd-master 是否计算 Homogeneous condition，不影响 scheduler
- 若 `disabled`，nfd-master 跳过 feature hash 计算，始终使用逐节点匹配

---

## 7. 多容器 Pod 兼容性 NFG 设计 — Image 粒度 NFG 方案

### 问题背景

Pod 通常包含多个容器（app + init containers + sidecars），每个容器可能使用不同的镜像。如何为多容器 Pod 生成兼容性 NFG 是一个核心架构决策，涉及两种粒度的权衡:

- **Image 粒度 NFG**: 每个 image digest 创建一个 NFG，复用率高，但引用计数管理复杂，Pod 异常退出时可能导致 refcount 泄漏
- **Pod 粒度 NFG**: 每个 Pod 创建一个 NFG，生命周期通过 ownerReference 自动管理，但无法跨 Pod 复用相同镜像的兼容性结果

### 设计方案: Image 粒度 NFG + Filter 取交集

直接使用 Image 粒度的 NFG，每个 image digest 对应一个 NFG CR。多容器 Pod 在 Filter 阶段对各镜像 NFG 的 status.nodes 取交集，得到最终兼容节点集合。

```
每个 image digest → 1 个 NFG CR

NFG-compat-sha256:aaa:
  spec: {kernel>=6, avx2}
  status.nodes: [node-1..node-500]
  annotations:
    refcount: "3"
    last-used: <timestamp>

NFG-compat-sha256:bbb:
  spec: {kernel>=5}
  status.nodes: [node-1..node-800]
  annotations:
    refcount: "2"
    last-used: <timestamp>
```

### NFG CR 定义

```yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: NodeFeatureGroup
metadata:
  name: compat-sha256-aaa123      # 名称 = "compat-" + image digest 前 12 位
  labels:
    nfd.k8s-sigs.io/image-digest: "sha256:aaa123..."
  annotations:
    nfd.k8s-sigs.io/refcount: "3"
    nfd.k8s-sigs.io/last-used: "2026-06-12T10:00:00Z"
spec:
  featureGroupRules:
    - name: "image-compatibility"
      matchFeatures:
        - feature: kernel.version
          matchExpressions:
            major: {op: In, value: ["6"]}
        - feature: cpu.cpuid
          matchExpressions:
            AVX2: {op: Is, value: true}
status:
  nodes:
    - name: node-1
    - name: node-2
    - name: node-5
```

### 调度流程

```
Prefilter 阶段:
  for each container image in Pod:
    digest = resolve(image)
    查 K8s API: NFG-compat-{digest} 是否存在?
      有 → refcount++, 更新 last-used
      无 → 创建 NFG-compat-{digest} (refcount=1)
           → 触发节点匹配计算 → 写入 status.nodes

Filter 阶段:
  compatibleNodes = ∩ (所有镜像 NFG 的 status.nodes)
  
  例: Pod 有 image-aaa + image-bbb
  NFG-aaa.status.nodes = [node-1..node-500]
  NFG-bbb.status.nodes = [node-1..node-800]
  compatibleNodes = [node-1..node-500]  ← 交集
  
  候选节点 ∩ compatibleNodes → 最终候选
```

### 缓存一致性

不需要主动管理。NFG status 是 K8s controller 的响应式循环:

```
节点特征变化
  → NFD worker 上报新特征
  → nfd-master 更新 NodeFeature
  → 触发所有相关 NFG 的 status 重新计算
     - admin pre-group NFG: 重新计算分组
     - image compat NFG: 重新计算兼容节点列表
  → status.nodes 自动更新
  → scheduler 下次 Filter 时读到的就是最新数据
```

这就是 K8s controller 模式的天然优势: **声明式 + 响应式，不需要手动管理缓存一致性。**

### GC 策略

```
Pod 完成/失败时:
  Informer 监听到 Pod 终态
  → 对应 NFG refcount--

GC Controller (定期扫描):
  for each Image Compat NFG:
    if refcount == 0 && (now - last-used) > TTL:
      删除 NFG CR
```

Pod 异常退出导致 refcount 没递减? TTL 兜底。

### 方案对比

| 维度 | Image 粒度 NFG (推荐) | Pod 粒度 NFG | 两层架构 |
|------|---------------------|-------------|---------|
| CRD 数量 | 1 (复用现有 NFG Kind) | 1 | 2 (ImageCompatibilityCache + NFG) |
| 1000 相同 Pod 的 CR 数 | 1 | 1000 | 1 + 1000 |
| 引用计数复杂度 | 中（image digest 级别） | 低（ownerRef） | 高（mergedSpec 级别） |
| 缓存一致性 | K8s 声明式天然保证 | K8s 声明式天然保证 | 需手动维护 |
| 节点特征变化响应 | NFG status 自动重算 | NFG status 自动重算 | 需通知缓存失效 |
| 多镜像处理 | Filter 取交集（镜像数通常 2-5 个，开销可忽略） | Pod 内一次性计算 | Prefilter 合并 spec |
| GC | TTL 兜底 | ownerRef 自动 GC | TTL 兜底 |
| 故障恢复 | 无需恢复，CR 持久化 | 无需恢复，CR 持久化 | 需重建内存缓存 |
| 多 scheduler 实例 | 天然共享 | 天然共享 | 需同步缓存 |

### 关键设计决策

1. **不引入新 CRD**: 复用现有 NodeFeatureGroup Kind，通过 label 区分 admin pre-group NFG 和 image compat NFG。

2. **Filter 阶段取交集**: 多容器 Pod 在 Filter 阶段对各镜像 NFG 的 status.nodes 取交集。镜像数通常 2-5 个，交集计算开销可忽略。

3. **无兼容性元数据的镜像**: 直接跳过，不创建 NFG CR。等价于"该镜像对节点无兼容性要求"。

4. **缓存未命中时的行为**:
   - `failurePolicy: Ignore` → 跳过该镜像的兼容性检查，继续调度
   - `failurePolicy: Fail` → 标记 Pod unschedulable，等待 NFG 创建完成后 requeue

5. **NFG 命名**: `compat-` + image digest 前 12 位，确保唯一性和可追溯性。

6. **RBAC 分离**: 通过 label `nfd.k8s-sigs.io/nfg-type: image-compat` 区分，scheduler plugin 只管理 image compat NFG，nfd-master 只管理 admin pre-group NFG。

---

## 8. NFG Status 计算归属 — Plugin + Master 协作方案

### 问题背景

NFG status 的计算由谁执行？这直接影响调度热路径延迟和系统架构复杂度。

三种可选方案:

| 方案 | 首次计算 | 后续更新 | 优点 | 缺点 |
|------|---------|---------|------|------|
| A: 纯 Plugin 侧 | Plugin 本地计算 + 写 status | Plugin 不感知节点变化 | 调度热路径无等待 | 节点变化后 status 过时 |
| B: 纯 Master 侧 | nfd-master 计算 status | nfd-master 响应式更新 | 单一 writer，符合 controller 模式 | 异步等待链，rate-limit 瓶颈 |
| C: Plugin + Master 协作 | Plugin 本地计算 + 首次写 status | nfd-master 响应式更新 | 首次无等待 + 后续自动更新 | 两个 writer，需协调 |

### 推荐方案: C（Plugin + Master 协作）

```
┌─────────────────────────────────────────────────────────────────────┐
│  触发源 1: 新 image 首次调度（Plugin 侧）                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Prefilter:                                                          │
│    1. 解析 image digest                                              │
│    2. 查 K8s API: image compat NFG 是否存在?                         │
│    3. 不存在 → 创建 NFG CR (spec = 兼容性规则)                       │
│    4. 读取所有 admin pre-group NFG:                                  │
│       for each pre-group:                                            │
│         读取 Homogeneous condition                                   │
│         if True  → 代表节点匹配 → O(1)                               │
│         if False → 逐节点匹配 → O(组内节点数)                        │
│    5. 处理未分组节点 (residual set):                                  │
│       ungroupedNodes = allNodes - ∪(pre-group status.nodes)          │
│       for each ungrouped node → 逐节点匹配                          │
│    6. 计算结果写入 NFG status.nodes                                  │
│    7. 标记 annotation: status-initialized: "true"                    │
│                                                                      │
│  Filter:                                                             │
│    读 NFG status.nodes（已就绪，无等待）                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  触发源 2: 节点特征变化（Master 侧）                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  节点特征变化                                                        │
│    → NFD worker 上报                                                 │
│    → nfd-master 更新 NodeFeature                                     │
│                                                                      │
│  Step 1: 更新 admin pre-group NFG                                    │
│    → 重新计算每个 pre-group 的 status.nodes                          │
│    → 重新计算 per-group feature hash                                 │
│    → 更新 Homogeneous condition (True/False)                         │
│                                                                      │
│  Step 2: 如果 Homogeneous 翻转 或 节点成员变化                        │
│    → 遍历所有 image compat NFG (status-initialized: "true")          │
│    → 对每个 image compat NFG:                                        │
│        重新执行同构性感知匹配（共享 NFD matcher 库）                   │
│        + 处理未分组节点 (residual set)                                │
│        更新 status.nodes                                             │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 两个 Writer 的协调

通过 `status-initialized` annotation 协调:
- Plugin 首次写入后标记 `status-initialized: "true"`
- nfd-master 只处理已标记的 NFG 的后续更新
- 避免 Plugin 和 Master 同时写同一个 NFG

### 写放大分析

Master 侧触发源 2 的写放大:

```
每次节点特征变化:
  更新 pre-group NFG: O(P) 次写入 (P = pre-group 数量)
  更新 image compat NFG: O(I) 次写入 (I = image compat NFG 数量)
  总写入: O(P + I)

实际场景:
  P ≈ 10-50 (pre-group 数量)
  I ≈ 20-200 (去重后的 image digest 数量)
  节点特征变化频率: 低（软件升级/硬件变更，非每分钟发生）
  
  → 写放大可接受
```

### 关键设计点

1. **Plugin 和 Master 共享 NFD matcher 库**: 同构性感知的匹配逻辑只实现一次，Plugin 和 Master 都引用同一个库。

2. **`status-initialized` annotation 协调两个 writer**:
   - Plugin 首次写入后标记 `status-initialized: "true"`
   - Master 只处理已标记的 NFG 的后续更新
   - 避免 Plugin 和 Master 同时写同一个 NFG

3. **Master 遍历所有 image compat NFG**: 不需要维护 pre-group → image compat NFG 的反向索引。因为 image compat NFG 数量（去重后的 image digest 数）通常远小于节点数，全量遍历开销可接受。

4. **Homogeneous 翻转是低频事件**: 正常情况下 Homogeneous=True 不会频繁变化，所以触发源 2 的全量重算不会频繁发生。

---

## 9. 同构性（Homogeneous）机制修正

### 原描述的问题

KEP 文档中原有的描述:
> "若 Homogeneous=False，降级到方案 A（对该组逐节点扫描）"

这个描述不准确。Homogeneous 不是 scheduler 的"降级开关"，而是 **NFG status 计算逻辑的内在部分**。

### 修正后的理解

```
同构性校验 = NFG status 计算逻辑的一部分，不是调度时的独立步骤

计算 image compat NFG 的 status.nodes（无论谁执行）:

  status.nodes = []
  for each admin pre-group NFG:
    if Homogeneous == True:
      取代表节点 → 用 image compat NFG 的 spec 匹配 → O(1)
      if 匹配 → 该组所有节点加入 status.nodes
      if 不匹配 → 跳过该组
    if Homogeneous == False:
      对该组每个节点 → 用 image compat NFG 的 spec 逐节点匹配 → O(组内节点数)
      匹配的节点加入 status.nodes
```

### 两个触发源都使用同一段逻辑

| 触发源 | 执行者 | 触发条件 | 计算范围 |
|--------|--------|---------|---------|
| 新 image 首次调度 | Plugin | Prefilter 发现 image compat NFG 不存在 | 单个 NFG |
| 节点特征变化 / Homogeneous 翻转 | Master | NodeFeature 更新 → 触发 pre-group NFG 重算 → Homogeneous 可能翻转 | 所有 image compat NFG |

### Homogeneous 翻转场景示例

```
初始状态:
  PreGroup-A: Homogeneous=True, nodes=[node-1..node-100]
  ImageCompat-X: status.nodes=[node-1..node-100]  (代表节点匹配成功)

场景: node-50 内核升级，特征漂移
  → Master 检测到 node-50 特征变化
  → 重算 PreGroup-A 的 feature hash → hash 变化
  → Homogeneous: True → False

  → 触发 ImageCompat-X 重算:
    PreGroup-A 现在 Homogeneous=False
    → 对 node-1..node-100 逐节点匹配 image compat spec
    → node-50 不再兼容（内核版本变了）
    → status.nodes = [node-1..node-49, node-51..node-100]  (去掉 node-50)

  → scheduler 下次调度时读到更新后的 status.nodes
```

### 对 KEP 文档的影响

| 原描述 | 修正 |
|--------|------|
| "若 Homogeneous=False，降级到方案 A（对该组逐节点扫描）" | 这不是 scheduler 的降级行为，而是 NFG status 计算时的内在逻辑 |
| "scheduler 检查 Homogeneous condition" | scheduler 不需要检查，只读 status.nodes |
| "同构性校验机制" 作为独立章节 | 移除，合并到 NFG status 计算逻辑中 |
| "homogeneityGuarantee: enforced 开关" | 保留，但仅控制 Master 是否计算 Homogeneous condition，不影响 scheduler |

### 调度后漂移处理

调度后漂移和同构性是两个独立的问题:

| 场景 | 问题 | 处理方式 |
|------|------|---------|
| 调度前漂移 | 调度时 NFG status 是否正确 | NFG 响应式更新，自动保证正确（见第 8 节） |
| 调度后漂移 | Pod 已在运行，节点特征变了 | 需要监控 + 告警 + 可选迁移 |

#### 调度后漂移的影响

```
时间线:
  T1: Pod 调度到 node-50（当时兼容）
  T2: node-50 内核升级，特征漂移
  T3: NFD worker 上报 → nfd-master 更新 NFG status
      node-50 从 image compat NFG 的 status.nodes 中移除
  
  问题: Pod 还在 node-50 上运行，但它已经"不兼容"了
```

#### 处理流程

```
1. 检测
   nfd-master 更新 NFG status 时:
     对比新旧 status.nodes
     找出"被移除的节点"
     检查这些节点上是否有使用相关镜像的 Pod

2. 告警
   生成 Event:
     kind: Pod
     reason: NodeCompatibilityDrift
     message: "Pod is running on node that drifted from
               compatibility requirements"

3. 处理选项（可配置: postDriftPolicy）
   - ignore (默认): 不干预，Pod 继续运行
   - taint: 给节点打 taint，阻止新 Pod 调度
   - deschedule: 触发 descheduler 迁移 Pod
```

#### 为什么默认是 ignore

1. **兼容性 ≠ 可用性**: 节点特征漂移不代表 Pod 会崩溃，只是"不再符合推荐的兼容性要求"
2. **迁移成本**: 强制迁移可能比继续运行风险更大
3. **人工判断**: 让管理员根据业务场景决定是否迁移

---

## 10. OCI Artifact 预解析 — Mutating Webhook + 内存缓存

### 问题背景

OCI Artifact 拉取（registry I/O）不能放在调度热路径上:
- Registry 延迟不可控，会直接影响 p99 调度延迟
- Registry rate limit 可能导致调度阻塞
- `imagePullSecrets` 在 scheduler plugin 中难以正确获取
- 镜像兼容性元数据更新（热加载）需要独立于调度流程处理

参考 sigstore policy-controller 和 Kyverno verify-images 的设计模式，采用 **Mutating Webhook + 内存缓存** 方案，在 Pod 创建时同步解析镜像兼容性元数据并创建 ImageCompat NFG CR。

### 参考设计分析

#### sigstore policy-controller 核心模式

- **ClusterImagePolicy CRD**: 集群级策略定义，包含 image glob 匹配模式 + authorities（签名验证配置）
- **Webhook 拦截**: Pod 创建时拦截，同步验证签名
- **SignaturePullSecrets**: 支持签名存储在不同 registry，使用独立 pull secrets
- **namespace 级别控制**: 通过 label（`policy.sigstore.dev/include: "true"`）选择启用范围
- **no-match-policy**: 可配置不匹配时的行为（warn/allow/deny）
- **多种 authority 类型**: key（静态公钥）、keyless（Fulcio 无密钥签名）、static（直接 pass/fail）
- **OCI Image Configuration**: 支持 `fetchConfigFile: true` 拉取镜像配置用于策略评估

#### Kyverno verify-images 核心模式

- **VerifyImagesRule**: 策略规则定义，支持 image 匹配 + 验证规则
- **Mutating webhook**: 支持将 image tag 替换为 digest（immutability）
- **imagePullSecrets**: 从 Pod namespace 或策略配置中获取
- **Failure policy**: `Ignore` / `Enforce` 两种模式

#### NFD 场景的适配

NFD image compatibility 与签名验证的关键差异:
- 不是验证签名，而是**提取兼容性元数据**并**持久化**为 ImageCompat NFG CR
- 需要**热加载**: 镜像兼容性元数据可能更新，采用惰性检查（TTL 过期时）
- 需要与 **nfd-master 协作**: webhook 填 spec，master 算 status.nodes
- **使用 webhook 同步解析**: 与 sigstore/Kyverno 一致，首批 Pod 即可调度

### 设计方案

#### 整体架构

```
Pod CREATE → apiserver → Mutating Webhook 拦截
  │
  ├─ 1. 提取所有 container image references
  ├─ 2. 对每个 image:
  │     查 webhook 内存缓存 (LRU):
  │       Hit (TTL 未过期) → 拿到 compatibility rules
  │       Hit (TTL 过期) → HEAD registry 检查 artifact-digest
  │         若变化 → 重新拉取 → 更新缓存
  │         若未变 → 刷新 TTL
  │       Miss → 同步拉取 OCI Artifact → 解析 → 写入缓存
  ├─ 3. 创建 ImageCompat NFG CR (spec 已填充):
  │     name = compat-sha256-{digest前12位}
  │     labels:
  │       nfd.k8s-sigs.io/nfg-type: image-compat
  │     annotations:
  │       nfd.k8s-sigs.io/artifact-digest: sha256:...
  │       nfd.k8s-sigs.io/image-ref: <原始镜像引用>
  │       nfd.k8s-sigs.io/last-resolved-at: <time>
  │       nfd.k8s-sigs.io/refcount: "0"
  │     spec.featureGroupRules: <解析出的兼容性规则>
  ├─ 4. 写入 Pod annotation:
  │     nfd.k8s-sigs.io/image-digests: "sha256:aaa,sha256:bbb"
  ├─ 5. 放行 Pod
  │
  └─ Webhook 延迟:
       缓存命中 → ~ms 级（API 操作）
       缓存未命中 → registry RTT + 解析（冷启动，仅首次）

        ↓

nfd-master watch 到新 NFG CR
  → 计算 status.nodes（同构性感知匹配，见第 8/9 节）

        ↓

Scheduler Plugin (Prefilter):
  读 Pod annotation 获取 image digests
  查 ImageCompat NFG status.nodes → 过滤节点
```

#### 与 sigstore policy-controller 的对照

| sigstore policy-controller | NFD Image Compatibility Webhook | 说明 |
|---------------------------|--------------------------------|------|
| ClusterImagePolicy CRD | ImageCompatibilityWebhookConfig ConfigMap | 策略配置（registry、pullSecrets、TTL） |
| Webhook 拦截 Pod 创建 | Mutating Webhook 拦截 Pod CREATE | 一致 |
| 验证签名 | 提取兼容性元数据 | 从 OCI Artifact 读取 NFD 定义的 metadata |
| 验证通过 → 放行 Pod | 解析完成 → 创建 ImageCompat NFG → 放行 | 持久化解析结果 |
| SignaturePullSecrets | imagePullSecrets 配置 | 支持 registry 认证 |
| no-match-policy (warn/allow/deny) | failurePolicy (Ignore/Fail) | 缓存未命中时的行为 |
| namespace label opt-in | namespaceSelector | 控制解析范围 |

#### 与 Kyverno verify-images 的对照

| Kyverno verify-images | NFD Image Compatibility Webhook | 说明 |
|----------------------|--------------------------------|------|
| VerifyImagesRule | ImageCompatibilityWebhookConfig | 策略规则 |
| Mutating webhook (tag→digest) | Webhook 解析 tag → digest + 兼容性元数据 | 确保按 digest 去重 |
| imagePullSecrets from Pod namespace | 从 Pod namespace 查找 dockerconfigjson Secret | 与 Kyverno 一致 |
| Failure policy: Ignore/Enforce | failurePolicy: Ignore/Fail | 一致 |

### 配置定义

#### ImageCompatibilityWebhookConfig (ConfigMap)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nfd-image-compat-webhook-config
  namespace: nfd-system
data:
  config.yaml: |
    namespaceSelector:
      matchLabels:
        nfd.k8s-sigs.io/compatibility-resolve: "true"
    registries:
      - pattern: "registry.example.com/**"
        pullSecretRefs:
          - namespace: nfd-system
            name: regcred
        cacheTTL: 1h
      - pattern: "**"
        pullSecretSource: pod-namespace
        cacheTTL: 30m
    failurePolicy: Ignore
    artifactType: "application/vnd.nfd.compatibility.v1"
    gcPolicy:
      idleTimeout: 1h
```

#### ImageCompat NFG (由 webhook 创建)

```yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: NodeFeatureGroup
metadata:
  name: compat-sha256-aaa123bb4567
  labels:
    nfd.k8s-sigs.io/nfg-type: image-compat
  annotations:
    nfd.k8s-sigs.io/artifact-digest: "sha256:artifact-xxx"
    nfd.k8s-sigs.io/image-ref: "registry.example.com/app@sha256:aaa123bb4567..."
    nfd.k8s-sigs.io/last-resolved-at: "2026-06-15T10:00:00Z"
    nfd.k8s-sigs.io/refcount: "3"
    nfd.k8s-sigs.io/last-used: "2026-06-15T10:05:00Z"
spec:
  featureGroupRules:
    - name: "image-compatibility"
      matchFeatures:
        - feature: kernel.version
          matchExpressions:
            major: {op: In, value: ["6"]}
        - feature: cpu.cpuid
          matchExpressions:
            AVX2: {op: Is, value: true}
status:
  nodes:
    - name: node-1
    - name: node-2
    - name: node-5
```

### 调度流程

```
Prefilter 阶段:
  读 Pod annotation: nfd.k8s-sigs.io/image-digests
  for each digest:
    查 K8s API: ImageCompat NFG (compat-sha256-{digest}) 是否存在?
      存在 → refcount++, 更新 last-used, 读 status.nodes
      不存在 → Webhook 未创建 NFG，scheduler 降级处理:
        1. 拉取 OCI Artifact (registry I/O)
        2. 解析兼容性元数据
        3. 创建 ImageCompat NFG CR (spec 已填充)
        4. 等待 nfd-master 计算 status.nodes (通过 requeue 机制)
        5. 下次调度周期读取 status.nodes

Filter 阶段:
  compatibleNodes = ∩ (所有镜像 NFG 的 status.nodes)
  候选节点 ∩ compatibleNodes → 最终候选
```

**降级调度的延迟影响:**
- 首次调度某镜像：增加 registry RTT + 解析时间 + nfd-master 计算时间
- 后续相同镜像：直接读取已创建的 NFG，无额外延迟
- 降级模式是临时状态，webhook 恢复后新 Pod 回到正常路径

### 故障域分离与降级机制

| 组件 | 职责 | 故障影响 |
|------|------|---------|
| Webhook | registry I/O + 解析 + 创建 NFG spec | 降级为 scheduler 阶段解析 |
| nfd-master | 根据 NFG spec 计算 status.nodes | NFG status 不更新，已有 status 仍可用 |
| Scheduler plugin | 读 NFG status → 过滤节点 | 无法执行兼容性过滤，按 failurePolicy 降级 |

#### Webhook 故障降级流程

```
正常路径:
  Pod CREATE → Webhook 解析 → 创建 NFG → Pod 创建成功
  → Scheduler 调度时读取 NFG status → 过滤节点

降级路径 (Webhook 故障):
  Pod CREATE → Webhook 超时/失败 → Pod 创建成功 (无 NFG)
  → Scheduler 调度时发现 NFG 不存在
  → Scheduler 降级为同步解析:
      1. 拉取 OCI Artifact
      2. 解析兼容性元数据
      3. 创建 ImageCompat NFG CR
      4. 等待 nfd-master 计算 status.nodes
      5. 继续调度流程
```

**降级模式的特点:**
- Scheduler plugin 需要具备 OCI Artifact 解析能力（与 webhook 共享解析库）
- 首次调度延迟增加（registry I/O 在调度热路径）
- 后续相同镜像的 Pod 可复用已创建的 NFG
- 不影响 Pod 可用性，但影响调度性能

#### Registry 不可达处理

**Webhook 阶段:**
- 拉取超时（timeoutSeconds: 5）
- 按 webhook failurePolicy:
  - Ignore → 放行 Pod，降级为 scheduler 阶段解析
  - Fail → 拒绝 Pod 创建

**Scheduler 降级阶段:**
- 拉取超时
- 按 scheduler failurePolicy:
  - Ignore → 跳过该镜像的兼容性检查，继续调度
  - Fail → 标记 Pod unschedulable，等待 registry 恢复

### 热加载流程（惰性检查）

```
Webhook 内存缓存 TTL 机制:
  每条缓存条目记录:
    - compatibility rules
    - artifact-digest
    - timestamp
  
  缓存命中时:
    if (now - timestamp) < cacheTTL:
      直接返回 compatibility rules
    else:
      HEAD registry → 获取当前 artifact-digest
      if 变化:
        重新拉取 OCI Artifact → 解析 → 更新缓存
        更新已有 NFG spec.featureGroupRules
        nfd-master Watch 到 spec 变化 → 自动重算 status.nodes
      else:
        刷新 timestamp
```

不需要后台 goroutine，**惰性检查**：只在缓存 TTL 过期且再次被访问时才检查更新。

### Webhook 配置

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: nfd-image-compat-resolver
webhooks:
  - name: resolve.nfd.k8s-sigs.io
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["pods"]
    namespaceSelector:
      matchLabels:
        nfd.k8s-sigs.io/compatibility-resolve: "true"
    failurePolicy: Ignore
    sideEffects: None
    timeoutSeconds: 5
```

### Metrics

```
webhook_resolve_total{status="success|failure|timeout"}  # 解析总次数
webhook_resolve_duration_seconds                          # 解析耗时分布
webhook_cache_hit_ratio                                   # 缓存命中率
webhook_registry_request_total{registry, status}          # registry 请求统计
webhook_cache_refresh_total{changed="true|false"}         # TTL 过期检查次数
webhook_gc_total                                          # GC 删除 NFG 次数
```

### 关键设计决策

1. **使用 Mutating Webhook 同步解析**: 与 sigstore policy-controller 和 Kyverno verify-images 一致，webhook 拦截 Pod 创建时同步解析镜像兼容性元数据并创建 NFG。首批 Pod 即可调度，无需等待 controller。

2. **内存 LRU 缓存**: Webhook 进程内维护 LRU 缓存，按 image digest 索引。1000 副本 Deployment 只有第 1 个 Pod 经历 registry RTT，后续 999 个全部命中缓存。

3. **惰性热加载**: 不需要后台 goroutine。缓存 TTL 过期时，下次访问该镜像才 HEAD registry 检查 artifact-digest 是否变化。减少不必要的 registry 请求。

4. **Image digest 不可变性**: Webhook 将 image tag 解析为 digest 写入 Pod annotation，保证调度时使用的 digest 与创建时一致（参考 Kyverno 的 tag→digest mutation）。

5. **复用 ImageCompat NFG**: 不引入新 CRD 存储解析结果。Webhook 创建的 NFG 与 scheduler 使用的 NFG 是同一个 CR，通过 label `nfd.k8s-sigs.io/nfg-type: image-compat` 区分。

6. **imagePullSecrets 策略**: 参考 sigstore 的 `SignaturePullSecrets`，支持两种模式:
   - `pod-namespace`: 从 Pod 所在 namespace 查找 dockerconfigjson Secret（默认）
   - `config-ref`: 从 ConfigMap 中指定的 Secret 引用

7. **Webhook 故障降级**: `failurePolicy: Ignore`（默认），webhook 故障时放行 Pod，scheduler 降级为同步解析并创建 NFG。保证功能可用性，但首次调度延迟增加。

---

## 11. 未分组节点处理 — 隐式 Residual Set

### 问题背景

集群中可能存在不属于任何 admin pre-group 的节点:

```
集群 100 个节点:
  80 个节点属于 admin pre-group (Group-A ~ Group-D)
  20 个节点不属于任何 pre-group

  如果镜像兼容某个未分组节点，但没被选中 → 调度遗漏
```

### 解决方案: 隐式 Residual Set

nfd-master 天然知道所有节点和所有已分组节点，**未分组节点 = 全部节点 - 所有 pre-group 的节点并集**。在计算 ImageCompat NFG 的 status.nodes 时，自动把未分组节点纳入:

```
计算 ImageCompat NFG 的 status.nodes:

  compatibleNodes = []

  // 第一步：遍历所有 admin pre-group（同构性感知匹配）
  for each admin pre-group NFG:
    if Homogeneous == True:
      代表节点匹配 → O(1)
    if Homogeneous == False:
      逐节点匹配 → O(组内节点数)
    匹配的节点加入 compatibleNodes

  // 第二步：处理未分组节点（逐节点匹配）
  ungroupedNodes = allNodes - ∪(所有 pre-group 的 status.nodes)
  for each node in ungroupedNodes:
    用 image compat spec 逐节点匹配
    匹配的节点加入 compatibleNodes

  status.nodes = compatibleNodes
```

### 为什么推荐这个方案

| 维度 | 隐式 residual set（推荐） | 明确排除 |
|------|------------------------|---------|
| 正确性 | 不会遗漏兼容节点 | 可能遗漏兼容节点 |
| 复杂度 | 低，nfd-master 已有全部节点信息 | 最低 |
| 管理员负担 | 无需额外操作 | 需要文档说明限制 |
| 性能影响 | 未分组节点通常少，逐节点匹配开销可忽略 | 无 |
| 用户体验 | 透明，管理员不需要关心分组覆盖率 | 可能导致"明明有兼容节点却调度失败" |

### 未分组节点的处理特点

1. **始终逐节点匹配**: 未分组节点没有同构性保证，不能用代表节点优化
2. **性能影响可控**: 未分组节点通常数量少（管理员会把大多数节点分组），逐节点匹配开销可忽略
3. **无需 scheduler 感知**: scheduler 只读 status.nodes，不关心节点是否来自 pre-group 还是 residual set

### 边界情况

```
极端情况 1: 所有节点都未分组
  → 退化为 Proposal A（全量逐节点扫描）
  → 功能正确，性能退化
  → 管理员应被提醒配置 pre-group

极端情况 2: 所有节点都已分组
  → residual set 为空，第二步跳过
  → 无额外开销
```

### 对文档各章节的影响

| 章节 | 影响 |
|------|------|
| 第 7 节（Image 粒度 NFG） | Filter 取交集逻辑不变，status.nodes 已包含未分组节点 |
| 第 8 节（Plugin + Master 协作） | 两个触发源的计算逻辑都增加 residual set 步骤 |
| 第 9 节（同构性） | 同构性只针对 pre-group，residual set 始终逐节点匹配 |
| Proposal C 描述 | "no compatible nodes" 结论现在覆盖全集群 |

---

## 12. 性能预算重新评估

### Reviewer 意见 (ArangoGutierrez, Jun 11, 2026)

> Do these budgets assume warm caches? A cold Prefilter includes a registry round-trip, an NFG create, and a wait for status through nfd-master's rate-limited updater, so 50ms p99 isn't reachable cold — and with the requeue mechanism, 'Prefilter latency' excludes the wait entirely. Suggest stating cache assumptions, adding a pod-arrival-to-bind p99 since that's what users observe, and defining the last column (success rate of what, within what deadline?).

### 问题分析

原 KEP 文档中的性能指标存在以下问题：

1. **未明确缓存假设**: 没有区分 warm cache（NFG 已存在）和 cold cache（首次调度新镜像）
2. **Prefilter Latency 定义模糊**: 使用 requeue 机制时，Prefilter 等待 NFG status 就绪的时间被排除在外
3. **缺少用户可观测指标**: 用户关心的是 Pod 从进入调度队列到成功绑定的端到端延迟
4. **成功率定义不清**: 没有说明"成功率"是什么的成功率，以及在什么时间窗口内

### 当前架构的关键路径分析

```
┌─────────────────────────────────────────────────────────────────────┐
│  Warm Path (NFG 已存在，绝大多数情况)                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Prefilter 阶段:                                                     │
│    1. 读取 Pod annotation (image digests) → ~0.1ms                  │
│    2. 查询 ImageCompat NFG (informer 缓存命中) → ~1-2ms/image       │
│    3. 读取 NFG status.nodes (informer 缓存) → ~0.1ms                │
│    总计: ~2-5ms (2-5 个镜像)                                        │
│                                                                      │
│  Filter 阶段:                                                        │
│    1. 对每个镜像 NFG 的 status.nodes 取交集 → ~0.5-2ms/image        │
│    2. 与候选节点列表取交集 → ~0.1ms                                 │
│    总计: ~1-10ms                                                     │
│                                                                      │
│  总调度延迟: ~5-20ms (不含排队时间)                                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  Cold Path (首次调度新镜像)                                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  场景 A: Webhook 正常                                                │
│    Webhook: registry HEAD → 拉取 OCI Artifact → 解析 → 创建 NFG     │
│    nfd-master: 计算 status.nodes                                    │
│    Scheduler: 等待 NFG status 就绪 (requeue)                        │
│    额外延迟: +50-200ms                                              │
│                                                                      │
│  场景 B: Webhook 故障，scheduler 降级                                │
│    Scheduler: 同步拉取 OCI Artifact → 创建 NFG → 等待 status        │
│    额外延迟: +500ms-2s                                              │
│                                                                      │
│  后续相同镜像: 0ms 额外延迟 (复用已创建的 NFG)                       │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 修正后的性能指标定义

| 指标 | 定义 | 说明 |
|------|------|------|
| **Prefilter Latency** | Prefilter 阶段执行时间（不含 requeue 等待） | 衡量 plugin 计算开销 |
| **Filter Latency** | Filter 阶段执行时间 | 衡量交集计算开销 |
| **Pod-Arrival-to-Bind** | Pod 进入调度队列到成功绑定的端到端延迟 | **用户可观测指标** |
| **Success Rate** | 在 5 秒时间窗口内成功绑定的 Pod 比例 | 明确时间窗口 |

### 修正后的性能目标

#### Warm Cache (NFG 已存在)

| 集群规模 | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind | 成功率 (50 pods/s, 5s 内) |
|---------|---------------|------------|-------------------------|---------------------------|
| 1k 节点 | < 5ms | < 5ms | < 50ms | 100% |
| 5k 节点 | < 10ms | < 10ms | < 100ms | 100% |
| 10k 节点 | < 20ms | < 20ms | < 200ms | 99.9% |

#### Cold Cache (首次调度新镜像)

冷路径延迟由三部分组成：registry I/O + NFG CR 创建 + nfd-master status 计算。由于使用 requeue 机制，Prefilter Latency 不包含等待时间，因此用 **Pod-Arrival-to-Bind** 衡量冷路径端到端延迟。

| 场景 | P99 Pod-Arrival-to-Bind | 延迟构成 | 说明 |
|------|------------------------|---------|------|
| Webhook 正常 (1k 节点) | < 300ms | registry RTT (~50-100ms) + OCI 解析 (~10-20ms) + NFG create (~20ms) + nfd-master 计算 (~50-100ms) + requeue 调度 (~50ms) | 绝大多数冷路径场景 |
| Webhook 正常 (5k 节点) | < 500ms | 同上 + nfd-master 计算时间增加 (~100-200ms) | nfd-master 需遍历更多节点 |
| Webhook 正常 (10k 节点) | < 800ms | 同上 + nfd-master 计算时间进一步增加 (~200-400ms) | 含 residual set 逐节点匹配 |
| Webhook 故障，scheduler 降级 (1k 节点) | < 1.5s | scheduler 同步 registry RTT (~200-500ms) + 解析 + NFG create + nfd-master 计算 + requeue | 降级模式，延迟显著增加 |
| Webhook 故障，scheduler 降级 (5k 节点) | < 2s | 同上 + nfd-master 计算时间增加 | |
| Webhook 故障，scheduler 降级 (10k 节点) | < 3s | 同上 + nfd-master 计算时间进一步增加 | |
| 后续相同镜像 (任意规模) | 同 warm path | 复用已创建的 NFG，无额外延迟 | 冷路径仅影响首次 |

**冷路径成功率目标:**

| 集群规模 | 冷路径成功率 (5s 内绑定) | 说明 |
|---------|------------------------|------|
| 1k 节点 | 100% | webhook 正常 + 降级模式均在 5s 内完成 |
| 5k 节点 | 100% | 同上 |
| 10k 节点 | 99.9% | 极端情况下 nfd-master 计算可能接近 5s 边界 |

**冷路径指标说明:**
- **Pod-Arrival-to-Bind**: 包含 requeue 等待时间，是用户实际感知的延迟
- **成功率**: 首次调度新镜像的 Pod 在 5s 内成功绑定的比例
- **后续相同镜像**: 冷路径只影响每个 image digest 的首次调度，后续 Pod 走 warm path

---

## 总结: 待解决事项

| # | 事项 | 负责人 | 优先级 |
|---|------|--------|--------|
| 1 | ~~评估归属: plugin 侧 vs nfd-master 侧~~ → 已采用 Plugin + Master 协作方案解决（见第 8 节） | Xunli-Yang | 已解决 |
| 2 | ~~多容器 Pod 语义: per-image-digest vs per-pod NFG~~ → 已采用 Image 粒度 NFG + Filter 取交集方案解决（见第 7 节） | Xunli-Yang | 已解决 |
| 3 | ~~未分组节点行为: residual set 或明确排除~~ → 已采用隐式 residual set 方案解决（见第 11 节） | Xunli-Yang | 已解决 |
| 4 | ~~性能预算: warm vs cold cache 假设~~ → 已重新评估，区分 warm/cold path，添加 pod-arrival-to-bind 指标（见第 12 节） | Xunli-Yang | 已解决 |
| 5 | ~~OCI 预解析 controller/webhook 实现~~ → 已设计 Image Compatibility Resolver controller（见第 10 节） | Xunli-Yang | 已解决 |
| 6 | ~~NFG Kind 分离（NodeCompatibilityQuery）~~ → 已采用 label 区分方案: `nfd.k8s-sigs.io/nfg-type: image-compat`，复用现有 NFG Kind（见第 7 节） | Xunli-Yang | 已解决 |
| 7 | 将所有已达成共识的 checklist 项落实到 KEP 文档正文 | Xunli-Yang | 高 |
| 8 | 添加归属决策章节并附 KubeCon meeting notes 链接 | Xunli-Yang | 低 |
| 9 | 从 OWNERS 中指定 sig-scheduling reviewer | ArangoGutierrez | 低 |
| 10 | ~~Homogeneous=False 时的智能降级（ChaoyiHuang 建议）~~ → 已修正: Homogeneous 是 NFG status 计算的内在逻辑，不是 scheduler 的独立校验步骤（见第 9 节） | Xunli-Yang | 已解决 |
| 11 | Feature gate 名称、metrics、rollback 方案 | Xunli-Yang | 中 |
