# KEP-2403 Review Comments & Solutions

## PR Overview

- **PR**: [kubernetes-sigs/node-feature-discovery#2403](https://github.com/kubernetes-sigs/node-feature-discovery/pull/2403)
- **Title**: Add NFD image compatibility scheduler proposal
- **Author**: Xunli-Yang, ChaoyiHuang
- **Status**: Open (Changes Requested)
- **Preferred Solution**: Proposal C (Node Pre-grouping)

---

## 1. ArangoGutierrez (Maintainer) — Jan 15, 2026

### Preferred Direction: Solution 3 (Node Pre-Grouping)

Reasons:
1. Aligns with real-world cluster management — operators already organize nodes into pools/groups
2. O(G) vs O(N) is critical at scale — 10 representative nodes vs 10,000 individual nodes
3. Simpler architecture — no SQL database needed
4. Progressive path — start with Solution 1 as MVP, then add Solution 3 optimizations

### Questions & Solutions

| # | Question | Author Response | Status |
|---|----------|----------------|--------|
| 1 | **Scheduler plugin location**: Should it be in `kubernetes-sigs/scheduler-plugins`? | Target: integrate into scheduler-plugins; initially incubate in NFD SIG. Later confirmed at Amsterdam KubeCon: host in NFD repo. | Resolved |
| 2 | **NFG lifecycle management**: Cleanup strategy for ephemeral CRs? | Temporary CRs from scheduler will be garbage-collected with TTL. Later refined to: Spec-based hash dedup + reference counting + configurable TTL. | Resolved |
| 3 | **Group homogeneity enforcement**: How to validate/enforce homogeneity? What about feature drift? | Administrators responsible for initial homogeneity. NFG updates handle drift. Monitoring mechanism needed for drifted nodes. | Partially resolved — needs verification mechanism |
| 4 | **OCI artifact fetch failure during Prefilter** | Degradation strategy: continue scheduling with warning (fail-open default). Later added configurable `defaultCompatibilityFailurePolicy`: Ignore (default) or Fail. | Resolved |
| 5 | **Stale NFG status or slow controller update** | Retry scheduling; consider last-second validation before binding. Scheduler requeue via Informer/Watch + MovePodToActiveQueue. | Resolved |
| 6 | **No matching groups** | Schedule retries until fail, log error. | Resolved |

### Missing KEP Sections

| Section | Status |
|---------|--------|
| Risks and Mitigations | Added |
| Graduation Criteria (Alpha/Beta/GA) | Added |
| Implementation Timeline / Milestones | Partially added |
| Alternatives Considered | Added |
| Goals / Non-Goals | Added (May 22 commit) |

---

## 2. ChaoyiHuang (Co-author) — Jan 20 & 23, 2026

### Comments & Solutions

| # | Comment | Solution | Status |
|---|---------|----------|--------|
| 1 | **Plugin hosting**: `scheduler-plugins` is out-of-tree; no guarantee distributions include it. NFD is already integrated in almost all distributions. | Confirmed at Amsterdam KubeCon (Apr 14, 2026): host in NFD repo. Reasons: (1) in-tree requires core binary changes + main release cycle; (2) scheduler-plugins out-of-tree has no release cycle guarantee with NFD; (3) NFD already in most distributions. | Resolved |
| 2 | **NFG reuse**: Multiple pods may reuse same image. Suggested configurable purge policy (1h/24h TTL) for CR reuse. | Spec-based hash deduplication: ephemeral NFGs named by hash of compatibility specs. Reference counting annotation. GC only after refcount=0 AND idle timeout elapsed (default 1h). | Resolved |
| 3 | **Smarter fallback** (May 22): When Homogeneous=False, distinguish whether drifted features affect compatibility filtering or not. | Worth investigating. Two cases: (1) drifted features do NOT affect compatibility → no fallback needed; (2) drifted features DO affect compatibility → fallback to per-node scan. | Open — needs design |

---

## 3. colvert (Reviewer) — May 11, 2026

### Comments & Solutions

| # | Comment | Solution | Status |
|---|---------|----------|--------|
| 1 | **Summary text**: Enrich background to mention workload adaptation complexity and deployment delays. | Background enriched in May 18 commit. | Resolved |
| 2 | **Solution C vs B**: Need arguments on why C is preferred over B. B allows pre-checks but requires topology management. | B introduces SQLite + PVC to NFD master — large infrastructure change, out of scope. C leverages existing NFG mechanism, complexity is in grouping strategy not storage backend. | Resolved |
| 3 | **Pre-grouping combinations**: Even with templates, combinations could remain high. | Grouping should use key compatibility dimensions (e.g., CPU arch + kernel version), not all feature combinations. | Partially resolved |
| 4 | **Pre-grouping failure**: Group declaration could be too restrictive — "the declaration of the group will be key". | Acknowledged. Administrator guidance needed for grouping strategy. | Partially resolved |
| 5 | **Node affinity vs NFD scheduler**: How does NFD scheduler behave when affinity/nodeSelector are set by workload vendors? | Compatibility filtering executes first in Filter phase; affinity/nodeSelector handled by native Kubernetes plugins afterwards. AND relationship. | Resolved |

---

## 4. ArangoGutierrez — Second Detailed Review (May 11, 2026)

### 6 Themes to Address Before LGTM

| # | Theme | Detail | Solution | Status |
|---|-------|--------|----------|--------|
| 1 | **Group homogeneity verification** | Representative-node sampling is the load-bearing correctness claim but delegated to admin with no verification. | Hash-based homogeneity verification in nfd-master; `Status.Conditions.Homogeneous=True/False`. If False, fallback to Proposal A (per-node scan) for that group. Optional `homogeneityGuarantee: enforced` toggle. | Agreed — needs doc update |
| 2 | **OCI fetch in Prefilter** | Registry I/O in scheduler hot path hits rate limits and ignores `imagePullSecrets`. | Pre-scheduler controller or admission webhook resolving compatibility once per image digest, cache for scheduler. Reference: sigstore policy-controller, Kyverno verify-images. | Agreed — to be completed |
| 3 | **NFG Kind overloading** | Admin-defined groups and ephemeral per-pod queries share one Kind with different lifecycles/writers/RBAC. | Separate Kind (e.g., `NodeCompatibilityQuery`) that scheduler owns and nfd-master ignores. Reference: Karpenter NodePool vs NodeClaim. | Agreed — further analysis needed |
| 4 | **Synchronization and write amplification** | Prefilter creates NFG, Filter reads `.status`, but status path through nfd-master is rate-limited (`rate.Limit(10)`, bucket 100). | If evaluation moves to plugin-side, bypasses this bottleneck. Otherwise need spec'd trigger field + node-granular update API. Spec-hash dedup so 1000-replica Deployment creates one CR. | Agreed — needs architecture decision |
| 5 | **Standard KEP sections** | Goals/Non-Goals, feature gate name, metrics, scalability targets with numbers, rollback. | Goals/Non-Goals added. Feature gate, metrics, rollback still needed. | Partially resolved |
| 6 | **Hosting decision** | Amsterdam sig-scheduling discussion lives only in PR comment — should be captured in doc. | Need to add hosting decision section with link to meeting notes. Need sig-scheduling reviewer from OWNERS. | Open |

---

## 5. ArangoGutierrez — Third Review (Jun 11, 2026)

### Overall Assessment

Proposal now reads as coherent Phase-2 direction. Most major concerns have agreed resolutions. Remaining work is folding checklist into doc text.

### New Open Questions

| # | Question | Detail | Suggested Approach | Status |
|---|----------|--------|--------------------|--------|
| 1 | **Evaluation ownership** | Step 2 ambiguous — does plugin run representative-node match and write NFG status, or does nfd-master do it? Two different architectures: plugin-side needs NodeFeature informers + NFD matcher library; master-side needs node-granular NFG update API with spec'd trigger. | Recommend plugin-side: reduces nfd-master load, avoids rate-limit bottleneck, scheduler already has NodeFeature informer. | Open |
| 2 | **Multi-container pod composition** | Pods have multiple images (app + init + sidecars). Per-image-digest NFG (Filter intersects statuses) or per-pod NFG? Decides spec-hash key and refcount granularity. | Recommend per-image-digest NFG for finer-grained reuse. Filter phase intersects results. Spec-hash key = image digest + compatibility spec. Images without compat metadata skip NFG creation. | Open |
| 3 | **Ungrouped nodes** | "No compatible nodes exist" conclusion only holds if every node belongs to some pre-group. Nothing requires groups to cover the cluster. | Introduce implicit residual set (ungrouped nodes → default group evaluated per-node), or document that ungrouped nodes are excluded from compatibility scheduling. | Open |
| 4 | **Performance budget assumptions** | Do budgets assume warm caches? Cold Prefilter includes registry RTT + NFG create + wait for status through rate-limited nfd-master — 50ms p99 not reachable cold. | Separate warm/cold path metrics. Add pod-arrival-to-bind end-to-end p99. Define what "success rate" means and within what deadline. | Open |

---

## 6. Xunli-Yang — Comprehensive Response (May 21, 2026)

### Action Item Checklist

| # | Item | Status |
|---|------|--------|
| 1 | Move mechanism details to Design Details section | Done |
| 2 | Add Goals and Non-Goals | Done |
| 3 | Refine phrasing | Done |
| 4 | Enrich background introduction | Done |
| 5 | Add affinity/node selector explanations | Done |
| 6 | Incorporate scheduler Requeue mechanism | Done |
| 7 | NFG Spec-based hash dedup + reference counting + delayed deletion | Done |
| 8 | Pre-grouping homogeneity validation mechanism | Done |
| 9 | OCI fetch: resolve once per image digest, cache for scheduler | To be completed |
| 10 | NFG RBAC and Lifecycle Separation | Further analysis needed |
| 11 | Fail-safe policy for metadata retrieval failure | Done |
| 12 | Quantify Test Plan and Graduation Criteria | Done |

### Key Technical Decisions (Agreed)

#### Homogeneity Validation (Pre-scheduling Drift)
- NFD-NFG: Hash-based homogeneity verification in nfd-master
- `Status.Conditions.Homogeneous=True/False`
- Scheduler: If True → use representative node; If False → fallback to Proposal A (evaluate all nodes in group)
- NFD needs node-granular NFG update API (not full node scan)

#### Homogeneity Validation (Post-scheduling Drift)
- Non-intervention policy for running pods
- Log event, trigger alert, apply label/taint, human intervention

#### NFG Lifecycle — Spec-based Hash Deduplication
- Ephemeral NFGs named by hash of compatibility requirement specs
- Reference counting in annotations (increment on schedule reference, decrement on completion)
- Configurable TTL (`ephemeral-nfg-idle-timeout`, default 1 hour)
- GC only after usage-count hits zero AND exceeds TTL

#### Scheduler Requeue
- If NFG Status not ready → mark Pod unschedulable and back off
- Listen to NFG update events via Informer/Watch
- Trigger `MovePodToActiveQueue` when status populated

#### Failure Policy
- `defaultCompatibilityFailurePolicy` parameter:
  - `Ignore` (Fail-open): Default. Log warning, proceed scheduling
  - `Fail` (Fail-closed): Mark Pod Unschedulable until Registry recovers

#### Homogeneity Config
- `NodeFeatureGroup.spec.homogeneityGuarantee: enforced` toggle
- If `disabled`, system performs pre-bind validation instead

---

## Summary: Open Items

| # | Item | Owner | Priority |
|---|------|-------|----------|
| 1 | Evaluation ownership: plugin-side vs nfd-master-side | Xunli-Yang | High |
| 2 | Multi-container pod semantics: per-image-digest vs per-pod NFG | Xunli-Yang | High |
| 3 | Ungrouped nodes behavior: residual set or explicit exclusion | Xunli-Yang | Medium |
| 4 | Performance budget: warm vs cold cache assumptions | Xunli-Yang | Medium |
| 5 | OCI pre-resolve controller/webhook implementation | Xunli-Yang | High |
| 6 | NFG Kind separation (NodeCompatibilityQuery) | Xunli-Yang | Medium |
| 7 | Fold all agreed checklist items into KEP doc text | Xunli-Yang | High |
| 8 | Add hosting decision section with KubeCon meeting notes link | Xunli-Yang | Low |
| 9 | Assign sig-scheduling reviewer from OWNERS | ArangoGutierrez | Low |
| 10 | Smarter fallback for Homogeneous=False (ChaoyiHuang's suggestion) | Xunli-Yang | Medium |
| 11 | Feature gate name, metrics, rollback plan | Xunli-Yang | Medium |
