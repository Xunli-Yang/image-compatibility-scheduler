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

#### NFG 响应式更新 — 预分组优化，无需 Homogeneous 字段

- **保留预分组机制用于性能优化**：将 O(N) 节点匹配优化为 O(G)（G = 分组数），显著减少 nfd-master 的计算时间
- **不引入 Homogeneous condition 显式字段**：nfd-master 内部隐式检测 pre-group 一致性，不一致时自动回退到逐节点匹配
- **调度器直接信任 status.nodes**：调度器在 Filter 阶段直接读取，无需任何额外校验

#### 调度前漂移处理（两层防护）
- **第一层: ICQ status 异步更新 (覆盖 99% 场景)**
  - 节点特征漂移 → NFD worker 上报 → nfd-master 更新 NodeFeature
  - Scheduler Plugin 通过 informer 监听到变化，重算 ICQ status.compatibleNodes
  - 漂移节点从 compatibleNodes 中移除，后续调度的 Pod 自动避开
- **第二层: PreBind 实时验证 (兜底 1% 竞态场景)**
  - 如果 informer 回调延迟，ICQ status 过时，PreBind 用节点最新特征做实时验证
  - 发现漂移 → 拒绝绑定 → 重新调度
  - PreBind 开销极小 (< 1ms)，保证调度的最终正确性
- **结果**: 新 Pod 不会调度到漂移节点

#### 调度后漂移处理（label + log 告警）
- 调度后漂移由 **Scheduler Plugin** 负责检测，因为 plugin 管理 ICQ 的完整生命周期
- 检测方式: Scheduler plugin 监听 NodeFeature 变化，重算 ICQ status.compatibleNodes 时，对比新旧列表，找出"被移除的节点"
- 检查被移除节点上是否有使用相关镜像的 Pod
- 对每个受影响的 Pod:
  - **给 Pod 打 label**:
    - `nfd.k8s-sigs.io/compatibility-drift: "true"`
    - `nfd.k8s-sigs.io/drift-node: "node-50"`
    - `nfd.k8s-sigs.io/drift-time: "2026-06-15T10:30:00Z"`
  - **记录结构化日志**: JSON 格式，包含 pod/node/image/drifted_features 详情
  - **生成 K8s Event** (可选): type=Warning, reason=NodeCompatibilityDrift
- 管理员通过 label/log/event 发现漂移，决定是否迁移
- **不做自动迁移**，避免侵入性操作（兼容性 ≠ 可用性，强制迁移可能比继续运行风险更大）

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

---

## 7. 多容器 Pod 兼容性 NFG 设计 — Image 粒度方案

### 问题背景

Pod 通常包含多个容器（app + init containers + sidecars），每个容器可能使用不同的镜像。如何为多容器 Pod 生成兼容性查询是一个核心架构决策，涉及两种粒度的权衡:

- **Image 粒度**: 每个 image digest 创建一个查询，复用率高，但引用计数管理复杂，Pod 异常退出时可能导致 refcount 泄漏
- **Pod 粒度**: 每个 Pod 创建一个查询，生命周期通过 ownerReference 自动管理，但无法跨 Pod 复用相同镜像的兼容性结果

### 设计方案: 独立 Kind + Image 粒度 + Filter 取交集

**引入新的 CRD: `ImageCompatibilityQuery`**，专门用于镜像兼容性查询，与 `NodeFeatureGroup` 分离。

#### 为什么需要独立 Kind

| 维度 | NodeFeatureGroup | ImageCompatibilityQuery |
|------|-----------------|------------------------|
| 语义 | 节点分组（管理员定义） | 兼容性查询（系统自动生成） |
| 生命周期 | 长期存在，管理员手动管理 | 临时存在，自动 GC |
| 创建者 | 集群管理员 | Webhook (spec only) / Scheduler Plugin |
| 更新者 | nfd-master (status.nodes) | Scheduler Plugin (status.compatibleNodes) |
| RBAC | 管理员权限 | 系统组件权限 |
| Spec 结构 | featureGroupRules（分组规则） | compatibilityRules（兼容性规则，内含 matchFeatures 复用 matcher 库） |

**关键洞察**: 虽然两者都产生 `status.nodes`，但它们的语义、生命周期、创建者完全不同。混合在一个 Kind 中会导致：
1. RBAC 难以精确控制
2. 生命周期管理混乱
3. 语义不清晰（分组 vs 查询）
4. 未来演进困难

#### CRD 定义

```yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: ImageCompatibilityQuery
metadata:
  name: icq-sha256-aaa123      # 名称 = "icq-" + image digest 前 12 位
  annotations:
    nfd.k8s-sigs.io/image-ref: "registry.example.com/app@sha256:aaa123..."
    nfd.k8s-sigs.io/refcount: "3"
    nfd.k8s-sigs.io/last-used: "2026-06-12T10:00:00Z"
spec:
  compatibilityRules:          # 语义清晰：兼容性规则
    - name: "image-compatibility"
      matchFeatures:           # 复用 NFD matcher 库的标准结构
        - feature: kernel.version
          matchExpressions:
            major: {op: In, value: ["6"]}
        - feature: cpu.cpuid
          matchExpressions:
            AVX2: {op: Is, value: true}
status:
  compatibleNodes:
    - name: node-1
    - name: node-2
    - name: node-5
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2026-06-12T10:00:00Z"
```

#### 与 NodeFeatureGroup 的对比

| 字段 | NodeFeatureGroup | ImageCompatibilityQuery |
|------|-----------------|------------------------|
| spec.featureGroupRules | 有（分组规则） | 无 |
| spec.compatibilityRules | 无 | 有（兼容性规则，内含 matchFeatures 复用 matcher 库） |
| status.nodes | 有（分组节点列表） | 无 |
| status.compatibleNodes | 无 | 有（兼容节点列表） |
| status.conditions | 无 | 有（Ready 状态） |

### 调度流程

```
Prefilter 阶段:
  for each container image in Pod:
    digest = resolve(image)
    查 K8s API: ImageCompatibilityQuery (icq-sha256-{digest}) 是否存在?
      存在 → refcount++, 更新 last-used, 读 status.compatibleNodes
      不存在 → 创建 ICQ (refcount=1)
           → 触发节点匹配计算 → 写入 status.compatibleNodes

Filter 阶段:
  compatibleNodes = ∩ (所有镜像 ICQ 的 status.compatibleNodes)
  
  例: Pod 有 image-aaa + image-bbb
  ICQ-aaa.status.compatibleNodes = [node-1..node-500]
  ICQ-bbb.status.compatibleNodes = [node-1..node-800]
  compatibleNodes = [node-1..node-500]  ← 交集
  
  候选节点 ∩ compatibleNodes → 最终候选
```

### 缓存一致性

不需要主动管理。ImageCompatibilityQuery status 由 Scheduler Plugin 响应式更新:

```
节点特征变化
  → NFD worker 上报新特征
  → nfd-master 更新 NodeFeature
  → Scheduler Plugin 通过 NodeFeature informer 监听到变化
  → 重新计算所有相关 ICQ 的 status.compatibleNodes
  → status.compatibleNodes 自动更新
  → scheduler 下次 Filter 时读到的就是最新数据
```

这就是 K8s informer 模式的天然优势: **声明式 + 响应式，不需要手动管理缓存一致性。**

### GC 策略

```
Pod 完成/失败时:
  Informer 监听到 Pod 终态
  → 对应 ICQ refcount--

GC Controller (定期扫描):
  for each ImageCompatibilityQuery:
    if refcount == 0 && (now - last-used) > TTL:
      删除 ICQ CR
```

Pod 异常退出导致 refcount 没递减? TTL 兜底。

### 方案对比

| 维度 | Image 粒度 ICQ (推荐) | Pod 粒度 ICQ | 复用 NFG Kind |
|------|---------------------|-------------|--------------|
| CRD 数量 | 2 (NFG + ICQ) | 2 (NFG + ICQ) | 1 (NFG) |
| 1000 相同 Pod 的 CR 数 | 1 | 1000 | 1 |
| 语义清晰度 | 高（分组 vs 查询分离） | 高 | 低（混合语义） |
| RBAC 精确性 | 高（独立 Kind） | 高 | 低（需要 label 过滤） |
| 生命周期管理 | 清晰（独立 Kind） | 清晰 | 混乱（混合生命周期） |
| 引用计数复杂度 | 中（image digest 级别） | 低（ownerRef） | 中 |
| 缓存一致性 | K8s 声明式天然保证 | K8s 声明式天然保证 | K8s 声明式天然保证 |
| 多镜像处理 | Filter 取交集（镜像数通常 2-5 个，开销可忽略） | Pod 内一次性计算 | Filter 取交集 |
| GC | TTL 兜底 | ownerRef 自动 GC | TTL 兜底 |

### 关键设计决策

1. **独立 CRD**: 引入 `ImageCompatibilityQuery` Kind，与 `NodeFeatureGroup` 分离，语义清晰，RBAC 精确。

2. **Filter 阶段取交集**: 多容器 Pod 在 Filter 阶段对各镜像 ICQ 的 status.compatibleNodes 取交集。镜像数通常 2-5 个，交集计算开销可忽略。

3. **无兼容性元数据的镜像**: 直接跳过，不创建 ICQ CR。等价于"该镜像对节点无兼容性要求"。

4. **缓存未命中时的行为**:
   - `failurePolicy: Ignore` → 跳过该镜像的兼容性检查，继续调度
   - `failurePolicy: Fail` → 标记 Pod unschedulable，等待 ICQ 创建完成后 requeue

5. **ICQ 命名**: `icq-` + image digest 前 12 位，确保唯一性和可追溯性。

6. **RBAC 分离**: 
   - 管理员管理 NodeFeatureGroup（创建/删除/更新）
   - Webhook/Scheduler Plugin 管理 ImageCompatibilityQuery（创建/删除/更新 status）
   - nfd-master 只更新 NodeFeatureGroup 的 status，不更新 ICQ 的 status

---

## 8. ImageCompatibilityQuery Status 计算归属 — Scheduler Plugin 管理方案

### 问题背景

ImageCompatibilityQuery (ICQ) status 的计算由谁执行？这直接影响调度热路径延迟和系统架构复杂度。

三种可选方案:

| 方案 | 首次计算 | 后续更新 | 优点 | 缺点 |
|------|---------|---------|------|------|
| A: 纯 Plugin 侧 | Plugin 本地计算 + 写 status | Plugin 监听 NodeFeature 变化，主动重新计算 | 职责清晰，无跨组件协调 | Plugin 需要 watcher |
| B: 纯 Master 侧 | nfd-master 计算 status | nfd-master 响应式更新 | 单一 writer，符合 controller 模式 | 异步等待链，rate-limit 瓶颈，职责不清 |
| C: Plugin + Master 协作 | Plugin 本地计算 + 首次写 status | nfd-master 响应式更新 | 首次无等待 + 后续自动更新 | 两个 writer，需协调 |

### 推荐方案: A（Scheduler Plugin 完全管理）

```
┌─────────────────────────────────────────────────────────────────────┐
│  触发源 1: 新 image 首次调度（Plugin 侧）                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Prefilter:                                                          │
│    1. 解析 image digest                                              │
│    2. 查 K8s API: ImageCompatibilityQuery 是否存在?                  │
│    3. 不存在 → 创建 ICQ CR (spec = 兼容性规则)                       │
│    4. 利用 admin pre-group (NodeFeatureGroup) 加速匹配:              │
│       for each admin pre-group:                                      │
│         取代表节点 → 用 ICQ 的 spec 匹配                             │
│         if 匹配 → 该组所有节点加入 status.compatibleNodes            │
│         if 不匹配 → 跳过该组                                         │
│    5. 处理未分组节点 (residual set):                                  │
│       ungroupedNodes = allNodes - ∪(pre-group status.nodes)          │
│       for each ungrouped node → 逐节点匹配                          │
│    6. 计算结果写入 ICQ status.compatibleNodes                        │
│    7. 设置 ICQ status.conditions[Ready] = True                       │
│                                                                      │
│  Filter:                                                             │
│    读 ICQ status.compatibleNodes（已就绪，无等待）                    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  触发源 2: 节点特征变化（Plugin 侧）                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  节点特征变化                                                        │
│    → NFD worker 上报                                                 │
│    → nfd-master 更新 NodeFeature                                     │
│    → Scheduler plugin 通过 NodeFeature informer 监听到变化           │
│                                                                      │
│  Step 1: Plugin 重算所有 ImageCompatibilityQuery                     │
│    → 遍历所有 ICQ (status.conditions[Ready] = True)                  │
│    → 对每个 ICQ:                                                     │
│        利用 pre-group 加速匹配（代表节点匹配）                        │
│        如果 pre-group 内部不一致，对该组逐节点匹配                   │
│        + 处理未分组节点 (residual set)                                │
│        对比新旧 status.compatibleNodes，检测漂移                     │
│        更新 status.compatibleNodes                                   │
│        生成 NodeCompatibilityDrift Event（如有漂移）                 │
│        应用 postDriftPolicy                                          │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Scheduler Plugin 的职责

- **首次计算**: 创建 ICQ spec 并计算 status.compatibleNodes
- **后续更新**: 监听 NodeFeature 变化，主动重新计算 ICQ status
- **漂移检测**: 对比新旧 status.compatibleNodes，生成 Event
- **GC 管理**: 管理 refcount 和 TTL，删除过期的 ICQ

### nfd-master 的职责（仅 NodeFeatureGroup）

- 收集节点特征，更新 NodeFeatureGroup 的 status.nodes
- 隐式检测 pre-group 内部一致性
- **不更新 ICQ status**

### 写放大分析

Plugin 侧的写放大:

```
每次节点特征变化:
  nfd-master 更新 NodeFeatureGroup: O(P) 次写入 (P = pre-group 数量)
  Scheduler plugin 更新 ImageCompatibilityQuery: O(I) 次写入 (I = 唯一 image digest 数量)
  总写入: O(P + I)

实际场景:
  P ≈ 10-50 (pre-group 数量)
  I ≈ 20-200 (去重后的 image digest 数量)
  节点特征变化频率: 低（软件升级/硬件变更，非每分钟发生）
  
  → 写放大可接受
```

### 关键设计点

1. **ICQ 完全由 Scheduler Plugin 管理**: 职责边界清晰，ICQ 是调度相关资源，由调度组件管理。

2. **Plugin 已有 NodeFeature informer**: 无需额外的跨组件通信，plugin 可以直接监听 NodeFeature 变化。

3. **避免 nfd-master 的额外负担**: nfd-master 专注于节点特征收集和 NodeFeatureGroup 更新，不处理 ICQ。

4. **漂移检测自然由 plugin 负责**: 因为 plugin 管理 ICQ 的完整生命周期，漂移检测是其职责的一部分。

5. **共享 NFD matcher 库**: Plugin 和 nfd-master 都引用同一个 matcher 库，确保匹配逻辑一致。

---

## 9. 预分组优化与漂移处理

### 核心原则: 预分组用于性能优化，ICQ 由 Scheduler Plugin 管理

**保留预分组机制（NodeFeatureGroup）用于加速节点匹配，ImageCompatibilityQuery 独立 Kind 专门用于镜像兼容性查询，由 Scheduler Plugin 负责更新。**

预分组（admin pre-group）的核心价值是将 O(N) 的节点匹配优化为 O(G)（G = 分组数量），显著减少节点匹配的计算时间。这对于大规模集群（如 10000+ 节点）尤为重要，因为过长的计算时间会导致 ICQ status 更新延迟，进而阻塞调度。

### 预分组的工作机制

```
管理员定义 pre-group (NodeFeatureGroup):
  PreGroup-A: kernel.version=6.x, cpu.arch=x86_64
  PreGroup-B: kernel.version=5.x, cpu.arch=x86_64
  ...

Scheduler plugin 计算 ImageCompatibilityQuery 的 status.compatibleNodes:
  for each admin pre-group (NodeFeatureGroup):
    取代表节点 → 用 ICQ 的 spec 匹配
    if 匹配 → 该组所有节点加入 status.compatibleNodes  (O(1))
    if 不匹配 → 跳过该组                                (O(1))
  
  处理未分组节点:
    for each ungrouped node → 逐节点匹配                (O(U))
  
  总复杂度: O(G + U)，其中 G = 分组数，U = 未分组节点数
```

### 为什么不引入 Homogeneous condition

| 原设计 | 问题 | 修正 |
|--------|------|------|
| `Status.Conditions.Homogeneous` 字段 | 需要调度器检查并处理，增加调度路径复杂度 | 不引入该字段 |
| 调度器根据 Homogeneous 降级 | 调度器不应该关心 pre-group 的内部状态 | 调度器直接信任 status.compatibleNodes |
| 显式的 feature hash 计算 | 增加计算开销 | Scheduler plugin 内部隐式检测 |

**关键洞察**: Scheduler plugin 在计算 ICQ status 时，会隐式检测 pre-group 内部节点特征是否一致（通过读取 NodeFeatureGroup 的 status.nodes 和 NodeFeature）。如果发现不一致（某些节点特征漂移了），plugin 会自动对该组使用逐节点匹配，而不是代表节点匹配。这个检测和处理逻辑在 plugin 内部完成，不需要暴露额外的字段。

### 预分组一致性的隐式检测

```
Scheduler plugin 计算 ICQ status 时:

  1. 读取 NodeFeatureGroup 的 status.nodes
  2. 读取相关 NodeFeature，比较组内节点特征是否一致:
     if 所有节点特征一致:
       使用代表节点匹配（快速路径）
     if 节点特征不一致:
       使用逐节点匹配（慢速路径，但保证正确性）
  3. 计算并更新 ICQ status.compatibleNodes
```

这个检测逻辑对调度器完全透明。调度器只关心 ImageCompatibilityQuery 的 status.compatibleNodes，不关心 pre-group 的内部状态。

### ICQ status 计算逻辑

```
计算 ImageCompatibilityQuery 的 status.compatibleNodes（由 Scheduler Plugin 负责）:

  status.compatibleNodes = []
  
  // 利用 pre-group 加速匹配
  for each admin pre-group:
    if pre-group 内部节点特征一致:
      取代表节点 → 匹配 ICQ spec
      if 匹配 → 该组所有节点加入 status.compatibleNodes
    else:
      // pre-group 内部不一致，逐节点匹配
      for each node in pre-group:
        if node.features 匹配 ICQ spec:
          加入 status.compatibleNodes
  
  // 处理未分组节点
  ungroupedNodes = allNodes - ∪(all pre-group status.nodes)
  for each node in ungroupedNodes:
    if node.features 匹配 ICQ spec:
      加入 status.compatibleNodes
```

### 调度后漂移检测

**漂移检测由 Scheduler Plugin 负责**，因为 plugin 管理 ICQ 的完整生命周期。

#### 为什么是 Scheduler Plugin 而不是 nfd-master

| 维度 | Scheduler Plugin | nfd-master |
|------|-----------------|-----------|
| 职责边界 | 管理 ICQ 的完整生命周期 | 只负责 NodeFeatureGroup 的 status 更新 |
| 数据访问 | 通过 NodeFeature informer 获取最新数据 | 所有 NodeFeature 的权威来源 |
| 性能影响 | 监听 NodeFeature 变化是已有机制 | 增加额外负担，职责不清 |
| 一致性 | 漂移检测与 ICQ 更新在同一组件 | 跨组件协调复杂 |

#### 检测流程

```
Scheduler Plugin 监听到 NodeFeature 变化，重算 ImageCompatibilityQuery 的 status.compatibleNodes 时:

  1. 计算新的 status.compatibleNodes（基于当前 NodeFeature，利用 pre-group 加速）
  2. 对比旧的 status.compatibleNodes:
     removedNodes = old.status.compatibleNodes - new.status.compatibleNodes
  3. 对于每个 removedNode:
     检查该节点上是否有 Pod 使用了该 ICQ 对应的镜像
     if 有:
       生成 Event:
         kind: Pod
         reason: NodeCompatibilityDrift
         message: "Pod <pod-name> is running on node <node-name> 
                   which no longer satisfies compatibility 
                   requirements for image <image-digest>"
  4. 更新 status.compatibleNodes
  5. 应用 postDriftPolicy（如配置）
```

#### 漂移处理策略

可配置的 `postDriftPolicy`:

| 策略 | 行为 | 适用场景 |
|------|------|---------|
| `ignore` (默认) | 仅生成 Event，不干预 Pod 运行 | 大多数场景。兼容性 ≠ 可用性，强制迁移风险可能更大 |
| `taint` | 给漂移节点打 taint，阻止新 Pod 调度 | 防止新的不兼容 Pod 被调度到该节点 |
| `deschedule` | 触发 descheduler 迁移受影响的 Pod | 对兼容性要求严格的场景（如 HPC、实时计算） |

#### 为什么默认是 ignore

1. **兼容性 ≠ 可用性**: 节点特征漂移不代表运行中的 Pod 会崩溃——只是节点不再满足推荐的兼容性配置
2. **迁移成本**: 强制迁移可能导致服务中断，风险可能比继续运行更大
3. **人工判断**: 让管理员根据具体业务场景决定是否迁移

---

## 10. OCI Artifact 预解析 — Mutating Webhook + ICQ CR 持久化

### 问题背景

OCI Artifact 拉取（registry I/O）不能放在调度热路径上:
- Registry 延迟不可控，会直接影响 p99 调度延迟
- Registry rate limit 可能导致调度阻塞
- `imagePullSecrets` 在 scheduler plugin 中难以正确获取
- 镜像兼容性元数据更新（热加载）需要独立于调度流程处理

参考 sigstore policy-controller 和 Kyverno verify-images 的设计模式，采用 **Mutating Webhook + ICQ CR 持久化** 方案，在 Pod 创建时同步解析镜像兼容性元数据并创建 ImageCompatibilityQuery CR。ICQ CR 本身就是持久化缓存，无需额外的内存缓存。

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
- 不是验证签名，而是**提取兼容性元数据**并**持久化**为 ImageCompatibilityQuery CR
- 需要**热加载**: 镜像兼容性元数据可能更新，采用惰性检查（TTL 过期时）
- 需要与 **nfd-master 协作**: webhook 填 spec，master 算 status.compatibleNodes
- **使用 webhook 同步解析**: 与 sigstore/Kyverno 一致，首批 Pod 即可调度

### 设计方案

#### 整体架构

```
Pod CREATE → apiserver → Mutating Webhook 拦截
  │
  ├─ 1. 提取所有 container image references
  ├─ 2. 对每个 image:
  │     解析 image → digest
  │     查 K8s API: ICQ `icq-{digest}` 是否存在?
  │       存在 → 直接复用，不需要创建
  │       不存在 → 同步拉取 OCI Artifact → 解析 → 创建 ICQ CR
  ├─ 3. ICQ CR 定义:
  │     name = icq-sha256-{digest前12位}
  │     annotations:
  │       nfd.k8s-sigs.io/image-ref: <原始镜像引用>
  │       nfd.k8s-sigs.io/artifact-digest: sha256:...
  │       nfd.k8s-sigs.io/last-resolved-at: <time>
  │       nfd.k8s-sigs.io/refcount: "0"
  │     spec.compatibilityRules: <解析出的兼容性规则>
  ├─ 4. 写入 Pod annotation:
  │     nfd.k8s-sigs.io/image-digests: "sha256:aaa,sha256:bbb"
  ├─ 5. 放行 Pod
  │
  └─ Webhook 延迟:
       ICQ 已存在 → ~ms 级（API 查询）
       ICQ 不存在 → registry RTT + 解析 + 创建 CR（仅首次）

          ↓

Scheduler Plugin (Prefilter):
  读 Pod annotation 获取 image digests
  查 ImageCompatibilityQuery 是否存在
  if 不存在:
    创建 ICQ spec (如果 webhook 未创建)
    计算 status.compatibleNodes（预分组加速匹配，见第 8/9 节）
  else:
    读取 status.compatibleNodes → 过滤节点
```

**设计要点：**
- **ICQ CR 本身就是持久化缓存** - 一旦创建，所有后续请求都能通过 API server 查到，无需额外的内存缓存
- **简化 webhook 代码** - 不需要维护 LRU 缓存逻辑，不需要考虑缓存一致性问题
- **多副本友好** - 多个 webhook 副本共享同一个 etcd 中的 ICQ，无需同步缓存状态
- **重启不丢失** - ICQ CR 持久化在 etcd 中，webhook 重启不影响

#### 与 sigstore policy-controller 的对照

| sigstore policy-controller | NFD Image Compatibility Webhook | 说明 |
|---------------------------|--------------------------------|------|
| ClusterImagePolicy CRD | ImageCompatibilityWebhookConfig ConfigMap | 策略配置（registry、pullSecrets、TTL） |
| Webhook 拦截 Pod 创建 | Mutating Webhook 拦截 Pod CREATE | 一致 |
| 验证签名 | 提取兼容性元数据 | 从 OCI Artifact 读取 NFD 定义的 metadata |
| 验证通过 → 放行 Pod | 解析完成 → 创建 ImageCompatibilityQuery → 放行 | 持久化解析结果 |
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
      - pattern: "**"
        pullSecretSource: pod-namespace
    failurePolicy: Ignore
    artifactType: "application/vnd.nfd.compatibility.v1"
```

#### ImageCompatibilityQuery (由 webhook 创建)

```yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: ImageCompatibilityQuery
metadata:
  name: icq-sha256-aaa123bb4567
  annotations:
    nfd.k8s-sigs.io/image-ref: "registry.example.com/app@sha256:aaa123bb4567..."
    nfd.k8s-sigs.io/artifact-digest: "sha256:artifact-xxx"
    nfd.k8s-sigs.io/last-resolved-at: "2026-06-15T10:00:00Z"
    nfd.k8s-sigs.io/refcount: "3"
    nfd.k8s-sigs.io/last-used: "2026-06-15T10:05:00Z"
spec:
  compatibilityRules:
    - name: "image-compatibility"
      matchFeatures:
        - feature: kernel.version
          matchExpressions:
            major: {op: In, value: ["6"]}
        - feature: cpu.cpuid
          matchExpressions:
            AVX2: {op: Is, value: true}
status:
  compatibleNodes:
    - name: node-1
    - name: node-2
    - name: node-5
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2026-06-15T10:00:00Z"
```

### 调度流程

```
Prefilter 阶段:
  读 Pod annotation: nfd.k8s-sigs.io/image-digests
  for each digest:
    查 K8s API: ImageCompatibilityQuery (icq-sha256-{digest}) 是否存在?
      存在 → refcount++, 更新 last-used, 读 status.compatibleNodes
      不存在 → Webhook 未创建 ICQ，scheduler 降级处理:
        1. 拉取 OCI Artifact (registry I/O)
        2. 解析兼容性元数据
        3. 创建 ImageCompatibilityQuery CR (spec 已填充)
        4. 等待 nfd-master 计算 status.compatibleNodes (通过 requeue 机制)
        5. 下次调度周期读取 status.compatibleNodes

Filter 阶段:
  compatibleNodes = ∩ (所有镜像 ICQ 的 status.compatibleNodes)
  候选节点 ∩ compatibleNodes → 最终候选
```

**降级调度的延迟影响:**
- 首次调度某镜像：增加 registry RTT + 解析时间 + Scheduler plugin 计算时间
- 后续相同镜像：直接读取已创建的 ICQ，无额外延迟
- 降级模式是临时状态，webhook 恢复后新 Pod 回到正常路径

### 故障域分离与降级机制

| 组件 | 职责 | 故障影响 |
|------|------|---------|
| Webhook | registry I/O + 解析 + 创建 ICQ spec | 降级为 scheduler 阶段解析 |
| nfd-master | 收集节点特征，更新 NodeFeatureGroup status.nodes | NodeFeatureGroup status 不更新，已有 status 仍可用 |
| Scheduler plugin | 计算并更新 ICQ status.compatibleNodes，读 ICQ status → 过滤节点 | 无法执行兼容性过滤，按 failurePolicy 降级 |

#### Webhook 故障降级流程

```
正常路径:
  Pod CREATE → Webhook 解析 → 创建 ICQ spec → Pod 创建成功
  → Scheduler plugin 计算 ICQ status → Scheduler 读取 ICQ status → 过滤节点

降级路径 (Webhook 故障):
  Pod CREATE → Webhook 超时/失败 → Pod 创建成功 (无 ICQ)
  → Scheduler 调度时发现 ICQ 不存在
  → Scheduler 降级为同步解析:
      1. 拉取 OCI Artifact
      2. 解析兼容性元数据
      3. 创建 ImageCompatibilityQuery CR (spec)
      4. Scheduler plugin 计算 status.compatibleNodes
      5. 继续调度流程
```

**降级模式的特点:**
- Scheduler plugin 需要具备 OCI Artifact 解析能力（与 webhook 共享解析库）
- 首次调度延迟增加（registry I/O 在调度热路径）
- 后续相同镜像的 Pod 可复用已创建的 ICQ
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

### 热加载流程（可选）

ICQ CR 本身不包含热加载机制。如果需要支持镜像兼容性元数据更新，可以采用以下方式：

**方案 1: 手动更新（推荐）**
- 管理员删除旧的 ICQ CR
- 下一个 Pod 创建时，webhook 会重新拉取 OCI artifact 并创建新的 ICQ

**方案 2: 后台定期检查（可选）**
```
Webhook 后台 goroutine (定期执行，如每小时):
  for each ICQ in cluster:
    1. 从 ICQ annotation 读取 artifact-digest
    2. HEAD registry 获取当前 artifact-digest
    3. 如果变化:
       - 重新拉取 OCI Artifact
       - 更新 ICQ spec.compatibilityRules
       - 更新 ICQ annotation artifact-digest
       - Scheduler Plugin Watch 到 spec 变化 → 自动重算 status.compatibleNodes
```

**设计要点：**
- ICQ CR 是持久化的，不需要内存缓存
- 热加载是可选功能，可以在后续版本中添加
- 首次实现可以只支持手动更新，简化实现

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
webhook_gc_total                                          # GC 删除 ICQ 次数
```

### 关键设计决策

1. **使用 Mutating Webhook 同步解析**: 与 sigstore policy-controller 和 Kyverno verify-images 一致，webhook 拦截 Pod 创建时同步解析镜像兼容性元数据并创建 ICQ。首批 Pod 即可调度，无需等待 controller。

2. **ICQ CR 即持久化缓存**: 无需内存 LRU 缓存。ICQ CR 持久化在 etcd 中，按 image digest 命名。1000 副本 Deployment 只有第 1 个 Pod 经历 registry RTT，后续 999 个直接查到已存在的 ICQ CR。多副本 webhook 共享同一个 etcd 中的 ICQ，无需同步缓存状态。

3. **热加载（可选）**: 首次实现可以只支持手动更新（删除旧 ICQ，让 webhook 重新创建）。后续可以添加后台定期检查 ICQ 的 artifact-digest 是否变化来实现自动热加载。

4. **Image digest 不可变性**: Webhook 将 image tag 解析为 digest 写入 Pod annotation，保证调度时使用的 digest 与创建时一致（参考 Kyverno 的 tag→digest mutation）。

5. **独立 CRD**: 创建 `ImageCompatibilityQuery` Kind，与 `NodeFeatureGroup` 分离，语义清晰，RBAC 精确。

6. **imagePullSecrets 策略**: 参考 sigstore 的 `SignaturePullSecrets`，支持两种模式:
   - `pod-namespace`: 从 Pod 所在 namespace 查找 dockerconfigjson Secret（默认）
   - `config-ref`: 从 ConfigMap 中指定的 Secret 引用

7. **Webhook 故障降级**: `failurePolicy: Ignore`（默认），webhook 故障时放行 Pod，scheduler 降级为同步解析并创建 ICQ。保证功能可用性，但首次调度延迟增加。

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
| **Prefilter Latency** | Prefilter 阶段执行时间（不含 requeue 等待） | 衡量 plugin 计算开销，包含 feature 匹配计算、ICQ status 更新 |
| **Filter Latency** | Filter 阶段执行时间 | 衡量交集计算开销，包含 nodeSelector/affinity 适配 |
| **Pod-Arrival-to-Bind** | Pod 进入调度队列到成功绑定的端到端延迟 | **用户可观测指标**，包含完整调度周期和排队时间 |
| **1000 Pods Scheduling Duration** | 并发调度 1000 个 Pod 的总时长 | 衡量批量调度性能，scheduler 异步并发处理 |

### 修正后的性能目标

#### Warm Cache (ICQ 已存在，informer 已预热)

| Cluster Size (Nodes) | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind | 1000 Pods Scheduling Duration |
| :--- | :--- | :--- | :--- | :--- |
| **1k** | < 50ms | < 20ms | < 200ms | < 60s |
| **5k** | < 100ms | < 50ms | < 500ms | < 120s |
| **10k** | < 200ms | < 100ms | < 1s | < 180s |

#### Cold Cache (首次调度新镜像)

冷路径延迟由三部分组成：registry I/O + ICQ CR 创建 + status 计算。由于使用 requeue 机制，Prefilter Latency 不包含等待时间，因此用 **Pod-Arrival-to-Bind** 衡量冷路径端到端延迟。

| Cluster Size (Nodes) | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind | 
| :--- | :--- | :--- | :--- |
| **1k** | < 500ms | < 20ms | < 1s |
| **5k** | < 1s | < 50ms | < 2s |
| **10k** | < 2s | < 100ms | < 4s |

**冷路径指标说明:**
- **Pod-Arrival-to-Bind**: 包含 requeue 等待时间，是用户实际感知的延迟
- **1000 Pods Scheduling Duration**: 并发调度 1000 个 Pod 的总时长，考虑 scheduler 并发能力
- **后续相同镜像**: 冷路径只影响每个 image digest 的首次调度，后续 Pod 走 warm path

---

## 13. Affinity/NodeSelector 适配

### 问题背景

当工作负载供应商设置了 affinity/nodeSelector 时，NFD scheduler 如何行为？兼容性过滤与现有的节点选择机制如何协同工作？

### 解决方案

兼容性调度插件与现有的 node affinity 和 node selector 机制协同工作。兼容性过滤在 Filter 阶段执行，产生 compatibleNodes 集合。然后原生 K8s 调度器插件应用 affinity/nodeSelector 规则。最终节点必须同时满足两个约束（取交集）。

### 工作流程

```
Filter 阶段:
  1. 兼容性插件执行兼容性过滤:
     compatibleNodes = ∩ (所有镜像 ICQ 的 status.compatibleNodes)
     例如: compatibleNodes = [node-1..node-500]

  2. 原生 K8s 插件执行 affinity/nodeSelector 过滤:
     affinityNodes = [node-300..node-800]  (由 Pod spec 中的 affinity 规则决定)

  3. 取交集:
     finalCandidates = compatibleNodes ∩ affinityNodes
                     = [node-300..node-500]

  4. 返回 finalCandidates 给调度框架
```

### 示例场景

| 场景 | 兼容性过滤结果 | Affinity 结果 | 最终候选节点 |
|------|--------------|--------------|-------------|
| 无 affinity | [1..500] | 全部节点 | [1..500] |
| 有 affinity | [1..500] | [300..800] | [300..500] |
| 无兼容节点 | [] | [300..800] | [] (调度失败) |
| 无交集 | [1..100] | [300..800] | [] (调度失败) |

### 设计要点

1. **向后兼容**: 现有的使用 affinity/nodeSelector 的 Pod spec 无需修改，兼容性过滤作为额外的约束叠加。

2. **执行顺序**: 兼容性过滤在 Filter 阶段执行，与 affinity/nodeSelector 过滤并行，最终取交集。

3. **语义清晰**: 节点必须同时满足兼容性要求和 affinity/nodeSelector 约束，两者是 AND 关系。

4. **无冲突**: 兼容性插件不修改 affinity/nodeSelector 的行为，只是在候选节点集合上增加额外的过滤条件。

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
