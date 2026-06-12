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
| 1 | **分组同构性验证** | 代表节点采样是正确性的核心支撑，但当前完全委托给管理员，没有验证机制。 | nfd-master 中基于 hash 的同构性验证；`Status.Conditions.Homogeneous=True/False`。若 False，降级到方案 A（对该组逐节点扫描）。可选 `homogeneityGuarantee: enforced` 开关。 | 已达成共识 — 需更新文档 |
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
| 1 | **评估归属** | 步骤 2 有歧义 — 是 plugin 执行代表节点匹配并写入 NFG status，还是 nfd-master 做？这是两种不同架构: plugin 侧需要 NodeFeature informers + NFD matcher 库；master 侧需要节点粒度 NFG 更新 API 和 spec 化的 trigger。 | 建议 plugin 侧: 减少 nfd-master 负载，避免 rate-limit 瓶颈，调度器已有 NodeFeature informer。 | 待定 |
| 2 | **多容器 Pod 组合** | Pod 有多个镜像（app + init + sidecar）。per-image-digest NFG（Filter 取交集）还是 per-pod NFG？这决定了 spec-hash key 和 refcount 粒度。 | 建议 per-image-digest NFG，实现更细粒度的复用。Filter 阶段取交集。spec-hash key = image digest + compatibility spec。无兼容性元数据的镜像跳过 NFG 创建。 | 待定 |
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

#### 同构性验证（调度前漂移）
- NFD-NFG: nfd-master 中基于 hash 的同构性验证
- `Status.Conditions.Homogeneous=True/False`
- 调度器: 若 True → 使用代表节点；若 False → 降级到方案 A（对该组逐节点评估）
- NFD 需要节点粒度的 NFG 更新 API（非全量节点扫描）

#### 同构性验证（调度后漂移）
- 对运行中的 Pod 采取不干预策略
- 记录事件、触发告警、应用 label/taint、人工介入

#### NFG 生命周期 — 基于 Spec 的 Hash 去重
- 临时 NFG 以兼容性需求规格的 hash 值命名
- Annotation 中记录引用计数（调度引用时递增，完成时递减）
- 可配置 TTL（`ephemeral-nfg-idle-timeout`，默认 1 小时）
- 仅当 usage-count 降至零且超过 TTL 后才触发 GC

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
- 若 `disabled`，系统改为执行 pre-bind 验证

---

## 总结: 待解决事项

| # | 事项 | 负责人 | 优先级 |
|---|------|--------|--------|
| 1 | 评估归属: plugin 侧 vs nfd-master 侧 | Xunli-Yang | 高 |
| 2 | 多容器 Pod 语义: per-image-digest vs per-pod NFG | Xunli-Yang | 高 |
| 3 | 未分组节点行为: residual set 或明确排除 | Xunli-Yang | 中 |
| 4 | 性能预算: warm vs cold cache 假设 | Xunli-Yang | 中 |
| 5 | OCI 预解析 controller/webhook 实现 | Xunli-Yang | 高 |
| 6 | NFG Kind 分离（NodeCompatibilityQuery） | Xunli-Yang | 中 |
| 7 | 将所有已达成共识的 checklist 项落实到 KEP 文档正文 | Xunli-Yang | 高 |
| 8 | 添加归属决策章节并附 KubeCon meeting notes 链接 | Xunli-Yang | 低 |
| 9 | 从 OWNERS 中指定 sig-scheduling reviewer | ArangoGutierrez | 低 |
| 10 | Homogeneous=False 时的智能降级（ChaoyiHuang 建议） | Xunli-Yang | 中 |
| 11 | Feature gate 名称、metrics、rollback 方案 | Xunli-Yang | 中 |
