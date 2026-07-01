# PR #2403 Reply
## 1. ✅ Evaluation Ownership

**Problem**: Who runs representative-node evaluation and writes ICQ status?

**Solution**: Clear separation of responsibilities:
- **Webhook**: Creates ICQ CR (spec only, no status) during Pod admission
- **Scheduler Plugin**: Computes and updates `status.compatibleNodes` for ICQs, performs PreBind validation
- **nfd-master**: Updates `NodeFeatureGroup` status for admin-defined pre-groups, manages homogeneity labels, and detects post-scheduling drift

**Rationale**: 
- Avoids registry I/O in scheduler hot path
- Scheduler plugin has tight integration with the scheduling lifecycle
- nfd-master already has NodeFeature informer, naturally handles drift detection

## 2. ✅ ICQ Status Computation

**Problem**: How is ICQ status computed efficiently?

**Solution**: Pre-group acceleration with representative node matching:
- For each pre-group NFG, check homogeneity label `nfd.k8s-sigs.io/homogeneous-for-{icq-name}`
- **Label = "true"** → representative node matching: if the representative node matches, all nodes in that group are added to `status.compatibleNodes`
- **Label = "false"/missing** → node-by-node matching: each node in the group is checked individually
- Ungrouped nodes are evaluated individually via residual set mechanism

**Performance**: O(G) complexity in the critical path (G = number of groups, G<<N).

## 3. ✅ Multi-container Pod Composition

**Problem**: How do multi-container pods compose?

**Solution**: 
- One ICQ per container image
- Intersection of all ICQ `status.compatibleNodes` in Filter phase
- Images without compatibility metadata are skipped (no ICQ created)

## 4. ✅ Performance Targets

**Warm Cache** (ICQ exists, informer warmed):

| Cluster Size | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind | 1000 Pods Duration |
| :--- | :--- | :--- | :--- | :--- |
| **1k** | < 50ms | < 20ms | < 100ms | < 10s |
| **5k** | < 100ms | < 50ms | < 200ms | < 20s |
| **10k** | < 200ms | < 100ms | < 500ms | < 50s |

**Cold Cache** (first scheduling of new image):

| Cluster Size | P99 Prefilter | P99 Filter | P99 Pod-Arrival-to-Bind |
| :--- | :--- | :--- | :--- |
| **1k** | < 500ms | < 20ms | < 1s |
| **5k** | < 1s | < 50ms | < 2s |
| **10k** | < 2s | < 100ms | < 4s |

## 5. ✅ Node Feature Drift Handling

**Drift Before Scheduling:**
- nfd-master detects feature drift and updates NFG status accordingly
- Drifted nodes are automatically removed from NFG status
- PreBind phase performs real-time validation using latest node features as final safety net

**Drift After Scheduling:**
- nfd-master detects drifted node features, evaluates which ICQs are affected by comparing drifted features against `spec.compatibilityRules`
- Finds pods bound to drifted nodes via ICQ references
- Alerts administrators through:
  - Pod labels: `nfd.k8s-sigs.io/compatibility-drift: "true"`, `nfd.k8s-sigs.io/drift-node`, `nfd.k8s-sigs.io/drift-time`
  - Structured logs: JSON format with pod/node/image/drifted_features details
  - K8s Events: Warning events with `reason: NodeCompatibilityDrift`
- No automatic migration is performed

## 6. ✅ Nodes Outside Pre-group

**Problem**: What happens to nodes outside every pre-group?

**Solution**: 
- Residual set mechanism: `ungroupedNodes = allNodes - ∪(all pre-group status.nodes)`
- Independent per-node matching (no representative node optimization)
- If all nodes are ungrouped, system degrades gracefully to full per-node scanning (O(N))

## 7. ✅ Other Resolved Issues

- **Group homogeneity**: ICQ-dimension-based homogeneity check with labels managed by nfd-master
- **ICQ Lifecycle**: Reference counting via annotation + TTL-based GC
- **Failure Policy**: Two-level policy (cluster-level default + per-pod override via annotation)
- **NFG Kind overloading**: Independent ICQ CRD
- **Standard KEP sections**: Added Goals/Non-Goals, Risks, Graduation Criteria, Test Plan

## Next Steps

1. Update KEP documentation with refined design
2. Implement MVP version
3. Performance testing validation

