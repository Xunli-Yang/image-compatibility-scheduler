# KEP-2403 Review 答复

针对 [PR #2403](https://github.com/kubernetes-sigs/node-feature-discovery/pull/2403) 中 reviewer 提出的问题的逐条答复。

---

## 1. 分组同构性（Proposal C）

> 代表节点采样是正确性的核心支撑，但这完全委托给了管理员，没有验证机制。漂移的节点（内核补丁、驱动容器重启、MIG 重配置）会悄无声息地将不兼容的 Pod 路由到错误的节点。我们能否添加一个验证循环（每组特征哈希，漂移时设置 Status.Conditions.Homogeneous=False），或者让 PreBind 验证成为必需而非可选？

**已解决 — 保留预分组优化，但不引入 Homogeneous condition 显式字段。**

我们决定保留预分组机制用于性能优化（将 O(N) 优化为 O(G)），但不引入 Homogeneous condition 等显式校验字段。原因如下：

1. **预分组用于加速 nfd-master 的 status 计算**：对于大规模集群（10000+ 节点），预分组的代表节点匹配可以将计算时间从秒级降低到毫秒级，避免 NFG status 更新延迟阻塞调度。

2. **不引入 Homogeneous condition 字段**：调度器不应该关心 pre-group 的内部状态。nfd-master 在更新 pre-group 时会隐式检测组内节点特征是否一致，如果不一致会自动对该组使用逐节点匹配。这个检测逻辑在 nfd-master 内部完成，对调度器完全透明。

3. **调度器直接信任 status.compatibleNodes**：调度器在 Filter 阶段直接读取 ImageCompatibilityQuery 的 status.compatibleNodes，无需任何额外校验。

**预分组一致性的隐式检测**：

```
nfd-master 更新 pre-group 时:
  1. 收集组内所有节点的当前特征
  2. 比较节点特征是否一致:
     if 所有节点特征一致:
       使用代表节点匹配（快速路径，O(1)）
     if 节点特征不一致:
       使用逐节点匹配（慢速路径，但保证正确性）
  3. 更新 pre-group 的 status.nodes
```

**调度后漂移检测由 Scheduler Plugin 负责**，因为 plugin 管理 ICQ 的完整生命周期：

| 维度 | Scheduler Plugin | nfd-master |
|------|-----------------|-----------|
| 职责边界 | 管理 ICQ 的完整生命周期 | 只负责 NodeFeatureGroup 的 status 更新 |
| 数据访问 | 通过 NodeFeature informer 获取最新数据 | 所有 NodeFeature 的权威来源 |
| 性能影响 | 监听 NodeFeature 变化是已有机制 | 增加额外负担，职责不清 |
| 一致性 | 漂移检测与 ICQ 更新在同一组件 | 跨组件协调复杂 |

**漂移检测流程**：

```
调度前漂移（两层防护）:
  第一层: ICQ status 异步更新 (覆盖 99% 场景)
    Scheduler plugin 监听到 NodeFeature 变化，重算 ICQ status.compatibleNodes
    → 漂移节点从 compatibleNodes 中移除
    → 后续调度的 Pod 自动避开漂移节点

  第二层: PreBind 实时验证 (兜底 1% 竞态场景)
    如果 informer 回调延迟，ICQ status 过时:
    → PreBind 用节点最新特征做实时验证
    → 发现漂移 → 拒绝绑定 → 重新调度
    → PreBind 开销极小 (< 1ms)，保证调度的最终正确性

调度后漂移（label + log 告警）:
  Scheduler plugin 监听到 NodeFeature 变化，重算 ICQ status.compatibleNodes 时:
    1. 计算新的 status.compatibleNodes
    2. 对比旧的 status.compatibleNodes:
       removedNodes = old - new
    3. 对于每个 removedNode:
       检查该节点上是否有 Pod 使用了该镜像
       if 有:
         - 给 Pod 打 label:
           nfd.k8s-sigs.io/compatibility-drift: "true"
           nfd.k8s-sigs.io/drift-node: "node-50"
           nfd.k8s-sigs.io/drift-time: "2026-06-15T10:30:00Z"
         - 记录结构化日志 (JSON 格式)
         - 生成 K8s Event (可选)
    4. 更新 status.compatibleNodes
```

**漂移处理策略**：
- **调度前漂移**: PreBind 实时验证兜底，保证新 Pod 不会调度到漂移节点
- **调度后漂移**: 仅告警（label + log + event），不做自动迁移
  - 兼容性 ≠ 可用性，强制迁移风险可能更大
  - 管理员通过 label 查询受影响的 Pod，决定是否手动迁移

---

## 2. Prefilter 中的 OCI 拉取

> 调度器热路径中的 Registry I/O 会触发速率限制，并忽略 Pod 的 imagePullSecrets。预调度控制器或 admission webhook 按 image digest 解析兼容性更符合云原生模式——参见 sigstore policy-controller 和 Kyverno verify-images。

**已解决 — Mutating Webhook + 内存 LRU 缓存。**

我们采用了 Mutating Webhook 设计，紧密遵循 sigstore policy-controller 和 Kyverno verify-images 的模式：

```
Pod CREATE → apiserver → Mutating Webhook
  1. 提取容器镜像引用
  2. 对每个镜像：
     - 检查内存 LRU 缓存（以 image digest 为键）
     - 命中（TTL 有效）→ 使用缓存的兼容性规则
     - 命中（TTL 过期）→ HEAD registry，检查 artifact-digest 是否变化
     - 未命中 → 拉取 OCI Artifact，解析，填充缓存
  3. 创建 ImageCompatibilityQuery CR（spec 已填充）
  4. 写入 Pod annotation：nfd.k8s-sigs.io/image-digests
  5. 放行 Pod
```

**关键特性：**
- **调度器热路径无 registry I/O**：Webhook 处理所有 registry 交互。
- **imagePullSecrets**：从 Pod namespace 解析（类似 Kyverno）或从 webhook 配置解析（类似 sigstore 的 `SignaturePullSecrets`）。
- **缓存**：进程内 LRU，可配置 TTL。1000 副本的 Deployment 只触发 1 次 registry 拉取；其余 999 次命中缓存。
- **热加载**：缓存 TTL 过期时惰性检查——HEAD registry 检测 artifact-digest 变化。无需后台 goroutine。

**Webhook 故障降级**：如果 webhook 宕机（`failurePolicy: Ignore`），Pod 在没有 ICQ 的情况下创建。调度器插件检测到缺失的 ICQ，降级为同步 OCI 解析 + ICQ 创建：

```
正常路径：Pod CREATE → Webhook 解析 → ICQ spec 创建 → Scheduler plugin 计算 status → 调度器读取 ICQ status
降级路径：Webhook 宕机 → Pod 创建（无 ICQ）→ 调度器检测到缺失 ICQ
         → 调度器执行同步 OCI 解析 → 创建 ICQ spec
         → Scheduler plugin 计算 status → 继续调度流程
```

这保证了即使 webhook 不可用时的功能可用性，代价是首次调度延迟增加。

---

## 3. NodeFeatureGroup Kind 过载

> 管理员定义的分组和临时的 per-pod 查询共享同一个 Kind。不同的生命周期、不同的写入者、不同的 RBAC。一个独立的 Kind（例如 NodeCompatibilityQuery）由调度器拥有、nfd-master 忽略，可以保持职责清晰。参考 Karpenter 的 NodePool vs NodeClaim。

**已解决 — 引入独立的 ImageCompatibilityQuery Kind。**

经过分析，我们选择引入新的 CRD `ImageCompatibilityQuery`，与 `NodeFeatureGroup` 分离：

| 维度 | NodeFeatureGroup | ImageCompatibilityQuery |
|------|-----------------|------------------------|
| 语义 | 节点分组（管理员定义） | 兼容性查询（系统自动生成） |
| 生命周期 | 长期存在，管理员手动管理 | 临时存在，自动 GC |
| 创建者 | 集群管理员 | Webhook / Scheduler |
| 更新者 | nfd-master | nfd-master（响应式更新 status） |
| RBAC | 管理员权限 | 系统组件权限 |
| Spec 结构 | featureGroupRules（分组规则） | compatibilityRules（兼容性规则，内含 matchFeatures 复用 matcher 库） |
| Status 字段 | status.nodes | status.compatibleNodes |

**理由：**
1. **语义清晰**: NodeFeatureGroup 用于节点分组，ImageCompatibilityQuery 用于镜像兼容性查询，职责分离。
2. **RBAC 精确**: 管理员管理 NFG，Webhook/Scheduler 管理 ICQ，权限边界清晰。
3. **生命周期独立**: NFG 长期存在，ICQ 临时存在且有自动 GC，混合在一个 Kind 中会导致管理混乱。
4. **未来演进灵活**: 两个 Kind 可以独立演进，不会相互影响。

**参考 Karpenter 的设计**:
- Karpenter 使用 `NodePool`（管理员定义）和 `NodeClaim`（系统生成）两个独立的 Kind
- 同样是为了分离"声明式配置"和"运行时资源"
- 我们的设计遵循相同的模式：NodeFeatureGroup（声明式分组）vs ImageCompatibilityQuery（运行时查询）

---

## 4. 同步与写放大

> Prefilter 创建 ICQ，Filter 读取其 .status，但通过 nfd-master 的 status 路径是单个速率限制的 workqueue（pkg/nfd-master/updater-pool.go:92，rate.Limit(10)，bucket 100）。两个问题：Prefilter 如何等待？能否通过 spec hash 去重，使 1000 副本的 Deployment 创建一个 CR 而不是 1000 个？

**两个问题都已解决。**

**问题 1：Prefilter 如何等待？**

在 webhook 设计下，Prefilter 在大多数情况下**不需要**等待：

- **热路径（绝大多数情况）**：Webhook 已经创建了 ICQ spec，Scheduler plugin 已经计算了 `status.compatibleNodes`。调度器直接从 informer 缓存读取 `status.compatibleNodes`。零等待。
- **冷路径（首次调度新镜像）**：Webhook 在 Pod admission 期间创建 ICQ spec。当调度器处理 Pod 时，Scheduler plugin 计算 `status.compatibleNodes`。如果 ICQ 不存在，调度器使用 requeue 机制：标记 Pod 为 unschedulable，退避，并在 ICQ status Informer 触发时 `MovePodToActiveQueue`。
- **降级路径（webhook 宕机）**：调度器自己创建 ICQ spec 并计算 status。这是唯一需要额外计算的路径，且只影响每个新 image digest 的第一个 Pod。

**问题 2：能否通过 spec hash 去重？**

可以——而且我们更进一步。ICQ 按 **image digest** 去重，而不是 spec hash：

- ICQ 名称 = `icq-sha256-{digest_prefix}`（image digest 的前 12 个字符）。
- 1000 副本的 Deployment 使用相同镜像，只创建**一个** ICQ CR。
- Webhook 在第一个 Pod admission 时创建 ICQ；后续 Pod 发现它已存在，只需递增 refcount。
- 对于包含不同镜像的多容器 Pod，每个镜像有自己的 ICQ。调度器在 Filter 阶段计算所有镜像 ICQ 的 `status.compatibleNodes` 的交集。

**写放大分析：**

```
每次节点特征变化：
  Admin pre-group NodeFeatureGroup 更新：O(P) 次写入（P = pre-group 数量，通常 10-50）
  ImageCompatibilityQuery 更新：O(I) 次写入（I = 唯一 image digest 数量，通常 20-200）
  总计：O(P + I)

节点特征变化是低频事件（软件升级、硬件变更）。
P 和 I 相对于 N（节点数）都很小。
→ 写放大可接受。
```

---

## 5. 评估归属

> 步骤 2 读起来像是插件自己运行代表节点匹配并写入临时 ICQ status，而讨论中同意的 requeue 机制让插件等待 nfd-master 填充 status——这是两种不同的架构。文档能否确定一种，并说明评估器从哪里读取原始特征？插件侧评估意味着调度器内需要 NodeFeature informer 和 NFD 的 matcher 库；master 侧意味着 Goals 中的节点粒度 ICQ 更新 API 需要一个 spec 化的触发器（临时 ICQ 中的一个字段？）。

**已解决 — ICQ 完全由 Scheduler Plugin 管理。**

我们确定了清晰的职责边界：

**ICQ 生命周期管理（Scheduler Plugin 侧）：**
- Webhook 创建 ImageCompatibilityQuery CR（仅 spec）。
- Scheduler plugin 计算并写入 `status.compatibleNodes`，使用：
  - **NodeFeature informer**（调度器中已存在，供其他插件使用）。
  - **NFD matcher 库**（共享代码，与 nfd-master 逻辑相同）。
- Scheduler plugin 监听 NodeFeature 变化，主动重新计算 ICQ status。
- Scheduler plugin 管理 refcount 和 GC。

**nfd-master 职责（仅 NodeFeatureGroup）：**
- 收集节点特征，更新 NodeFeatureGroup 的 status.nodes。
- 隐式检测 pre-group 内部一致性。
- **不更新 ICQ status**。

**为什么这样设计：**
- ICQ 是调度相关资源，由调度组件管理，职责边界清晰。
- Scheduler plugin 已有 NodeFeature informer，无需额外的跨组件通信。
- 避免了 nfd-master 的额外负担，保持 nfd-master 专注于节点特征收集。
- 漂移检测自然由 scheduler plugin 负责，因为它管理 ICQ 的完整生命周期。

**原始特征来源：**
- Scheduler plugin 从 NodeFeature informer 缓存读取（本地，无 API 调用）。
- nfd-master 从其内部 NodeFeature 存储读取（权威来源，用于更新 NodeFeatureGroup）。

**写入者协调：**
- `status.conditions[Ready]` 标记 ICQ 已准备好接受 master 侧更新。
- 插件在首次写入后设置；master 忽略未 Ready 的 ICQ。

---

## 6. 多容器 Pod 组合

> Pod 是多镜像的：app 容器加上 initContainers/sidecars，每个都有自己的 artifact 或没有。值得定义组合语义——每个 image digest 一个临时 ICQ（与已同意的 per-digest 解析一致），Filter 阶段取它们 status 的交集；或者每个 pod 级需求一个 ICQ，这会破坏去重。这也决定了 spec-hash 键和 refcount 粒度。

**已解决 — 每个 image digest 一个 ICQ，Filter 取 `status.compatibleNodes` 交集。**

- 每个 image digest 有自己的 ICQ CR：`icq-sha256-{digest_prefix}`。
- 多容器 Pod：调度器读取所有镜像 ICQ，在 Filter 阶段取它们 `status.compatibleNodes` 的交集。
- 没有兼容性元数据的镜像被跳过（不创建 ICQ，不应用过滤）。
- 这最大化了复用：一个被 50 个不同应用使用的基础镜像仍然只有一个 ICQ。

**示例：**

```
Pod 有 3 个容器：
  - app@sha256:aaa   → ICQ-aaa: status.compatibleNodes = [node-1..node-500]
  - init@sha256:bbb  → ICQ-bbb: status.compatibleNodes = [node-1..node-800]
  - sidecar@sha256:ccc → （无兼容性元数据，跳过）

Filter 计算：[node-1..node-500] ∩ [node-1..node-800] = [node-1..node-500]
```

**Refcount 粒度：** 按 image digest。每个 ICQ 的 refcount 跟踪当前引用该特定镜像的 Pod 数量。

---

## 7. 未分组节点

> 这个结论只有在每个节点都属于某个 pre-group 时才成立，而设计中没有要求分组覆盖整个集群。一个兼容但未分组的节点会把这变成错误的"不存在兼容节点"判定。建议明确定义未分组节点的行为——例如，一个隐式的 residual set 按节点评估，或者一个文档化的声明，说明未分组节点被排除在兼容性调度之外。

**已解决 — 隐式 residual set。**

ICQ status 计算自动包含未分组节点：

```
计算 ImageCompatibilityQuery 的 status.compatibleNodes：

  compatibleNodes = []

  // 第一步：遍历所有 admin pre-group（隐式同构性检测）
  for each admin pre-group NodeFeatureGroup:
    if pre-group 内部节点特征一致:
      代表节点匹配 → O(1)
    else:
      逐节点匹配 → O(组大小)
    将匹配的节点加入 compatibleNodes

  // 第二步：处理未分组节点（逐节点匹配）
  ungroupedNodes = allNodes - ∪(所有 pre-group 的 status.nodes)
  for each node in ungroupedNodes:
    与 ICQ spec 匹配
    将匹配的节点加入 compatibleNodes

  status.compatibleNodes = compatibleNodes
```

**特性：**
- 未分组节点始终按节点评估（没有代表节点优化，因为没有同构性保证）。
- 对调度器透明——它只读取 `status.compatibleNodes`，其中已经包含了匹配的未分组节点。
- nfd-master 天然知道所有节点和所有已分组节点，因此计算 residual set 很简单。

**边界情况：**
- 所有节点都未分组 → 退化为 Proposal A（全量逐节点扫描）。功能正确，性能退化。应提醒管理员。
- 所有节点都已分组 → residual set 为空，零开销。

---

## 8. 性能预算假设

> 这些预算是否假设了热缓存？冷的 Prefilter 包括 registry 往返、ICQ 创建、以及通过 nfd-master 速率限制更新器的 status 等待，所以 50ms p99 在冷启动下不可达——而且在 requeue 机制下，'Prefilter latency' 完全排除了等待时间。建议说明缓存假设，添加 pod-arrival-to-bind p99（因为这是用户观察到的），并定义最后一列（什么的成功率，在什么截止时间内？）。

**已解决 — 完整的热/冷路径分解与明确的指标定义。**

我们修订了性能目标，明确了缓存假设：

**指标定义：**
- **Prefilter Latency**：仅 Prefilter 阶段的时间。不包括 requeue 等待。
- **Filter Latency**：仅 Filter 阶段的时间。
- **Pod-Arrival-to-Bind**：从 Pod 进入调度队列到绑定的端到端延迟。**用户可观测指标。**
- **Success Rate**：在进入调度队列后 **5 秒内**绑定到兼容节点的 Pod 百分比。

**热缓存（ICQ 已存在，informer 已预热）：**

| 集群规模 | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind | 成功率 (50 pods/s, 5s 截止时间) |
|---------|---------------|------------|-------------------------|-------------------------------|
| 1k | < 5ms | < 5ms | < 50ms | 100% |
| 5k | < 10ms | < 10ms | < 100ms | 100% |
| 10k | < 20ms | < 20ms | < 200ms | 99.9% |

**冷缓存（首次调度新镜像）：**

| 场景 | P99 Pod-Arrival-to-Bind | 延迟分解 |
|------|------------------------|---------|
| Webhook 正常, 1k | < 300ms | registry RTT (~50-100ms) + OCI 解析 (~10-20ms) + ICQ 创建 (~20ms) + nfd-master 计算 (~50-100ms) + requeue (~50ms) |
| Webhook 正常, 5k | < 500ms | nfd-master 计算随节点数增加 |
| Webhook 正常, 10k | < 800ms | 包含 residual set 逐节点匹配 |
| Webhook 故障, 调度器降级, 1k | < 1.5s | 调度器中同步 OCI 拉取 |
| Webhook 故障, 调度器降级, 5k | < 2s | |
| Webhook 故障, 调度器降级, 10k | < 3s | |
| 后续相同镜像 | 同热路径 | ICQ 复用，零额外延迟 |

**冷缓存成功率：**

| 集群规模 | 成功率 (5s 截止时间) | 说明 |
|---------|---------------------|------|
| 1k | 100% | webhook 正常和降级模式都在 5s 内完成 |
| 5k | 100% | |
| 10k | 99.9% | nfd-master 计算接近 5s 边界的边缘情况 |

**关键洞察：** 冷路径只影响每个 image digest 的第一个 Pod。后续使用相同镜像的 Pod 走热路径。在典型工作负载中，初始部署后冷路径事件很少见。
