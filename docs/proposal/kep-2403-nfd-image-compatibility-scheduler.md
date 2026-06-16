#  KEP-2403: Image Compatibility Scheduler with NFD
<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
  - [Risks and Mitigations](#risks-and-mitigations)
    - [Group Homogeneity Enforcement](#group-homogeneity-enforcement)
    - [Node Features Drift Handling](#node-features-drift-handling)
    - [NFG Status Update Latency](#nfg-status-update-latency)
- [Design Details](#design-details)
  - [Proposal C: Node Pre-grouping](#proposal-c-node-pre-grouping)
  - [Test Plan](#test-plan)
  - [Graduation Criteria](#graduation-criteria)
    - [Alpha](#alpha)
    - [Beta](#beta)
    - [GA](#ga)
- [Implementation History](#implementation-history)
- [Alternatives Considered](#alternatives-considered)
  - [Use Node Affinity/Node Selector Directly in Pod Spec](#use-node-affinitynode-selector-directly-in-pod-spec)
  - [Alternative design proposals](#alternative-design-proposals)
    - [Proposal A: NodeFeatureGroup Check](#proposal-a-nodefeaturegroup-check)
    - [Proposal B: SQLite Database Caching for Node Features in Large Scale Clusters](#proposal-b-sqlite-database-caching-for-node-features-in-large-scale-clusters)
<!-- /toc -->

## Summary

Cloud-native technologies are being adopted by high-demand industries where container compatibility is critical for service performance and cluster preparation. The integration of workloads requiring specific resource adaptations (acceleration, specific networking behavior, ..) can quickly become complex and often involves multiple back-and-forths between the infrastructure teams and workload vendors. A convergence is usually necessary to align application needs with available resources. Experience shows that this is a significant cause of deployment delays.
Building upon the first phase of [KEP-1845 Proposal](https://github.com/kubernetes-sigs/node-feature-discovery/blob/master/enhancements/1845-nfd-image-compatibility/README.md), which completed node compatibility validation, this proposal introduces a compatibility scheduling plugin. This plugin introduces a new `ImageCompatibilityQuery` CRD to filter nodes that meet compatibility requirements, while leveraging existing `NodeFeatureGroup` for node pre-grouping optimization. It effectively schedules pods to compatible nodes, enabling automated and intelligent compatibility scheduling decisions to meet the application's need for a specific, compatible environment.

## Motivation

The first phase of [KEP-1845 Proposal](https://github.com/kubernetes-sigs/node-feature-discovery/blob/master/enhancements/1845-nfd-image-compatibility/README.md) introduced compatibility metadata to help container image authors describe compatibility requirements in a standardized way. This metadata is uploaded to the image registry alongside the image. Based on this container compatibility metadata, the compatibility scheduler plugin automatically analyzes the compatibility requirements of container images, filters suitable nodes for scheduling, and ensures that containers run on compatible nodes.

### Goals

- Implement an image compatibility scheduling plugin based on NFD to schedule Pods to compatible nodes, providing a production-ready scheduling extension for tracking image compatibility requirements.
- Introduce a new `ImageCompatibilityQuery` CRD to represent per-image compatibility queries, managed by the scheduler plugin.
- Leverage existing `NodeFeatureGroup` for node pre-grouping to optimize scheduling performance from O(N) to O(G) complexity.

### Non-Goals

- Making image compatibility scheduling plugin a hard requirement for the NFD usage.
- Cover applications ABI compatibility.

## Proposal

### User Stories

When deploying applications that require specific hardware or software features (e.g., AVX2 support, specific kernel versions, or GPU availability), users want to ensure that their pods are scheduled only on nodes that meet these compatibility requirements. This is particularly important for workloads in high-performance computing, machine learning, and other specialized domains where compatibility directly impacts performance and functionality.

### Risks and Mitigations
#### Group Homogeneity Enforcement
Group homogeneity is critical for proposal C to ensure that representative node checks accurately reflect the compatibility of the entire group. How to make sure that all nodes within a pre-group are actually homogeneous?

Cluster administrators are responsible for ensuring homogeneity when they define the pre-groups. It's mandatory for cluster administrators and up to the group strategy. It's similar to how node pools are managed in many large scale clusters. The scheduler plugin internally detects homogeneity by comparing node features when computing ICQ status, and falls back to per-node matching when inconsistency is detected. No explicit `Homogeneous` field is introduced to keep the design simple.

#### Node Features Drift Handling
When node features drift over time (e.g., due to software updates or hardware changes), it can lead to mismatches between the pre-group definitions and the actual node capabilities. This drift can compromise the effectiveness of the pre-grouping strategy.
It can be divided into two scenarios:
1. **Drift Before Scheduling:** The scheduler plugin recomputes ICQ status when it detects NodeFeature changes via informer. Drifted nodes are automatically removed from `compatibleNodes`. Additionally, the **PreBind phase** performs real-time validation using the latest node features, catching any race conditions where ICQ status might be stale.
2. **Drift After Scheduling:** When drift happens after a pod has been scheduled, the scheduler plugin detects affected pods and alerts administrators through:
   - **Pod labels**: `nfd.k8s-sigs.io/compatibility-drift: "true"`, `nfd.k8s-sigs.io/drift-node`, `nfd.k8s-sigs.io/drift-time`
   - **Structured logs**: JSON format with pod/node/image/drifted_features details
   - **K8s Events**: Warning events with `reason: NodeCompatibilityDrift`
   
   Administrators can query affected pods via label selector and decide whether to migrate (e.g., `kubectl drain`). No automatic migration is performed to avoid intrusive operations.

#### NFG Status Update Latency
If `NodeFeatureGroup` status updates are delayed, it can lead to stale information being used during the scheduling process. This latency can impact the accuracy of compatibility checks and potentially result in suboptimal scheduling decisions. However, since the pre-grouping can reduce the latency of NFG updates, the impact of this latency is limited. The **PreBind phase** provides a final validation step before binding, ensuring that any update latency is accounted for and stale status is caught before pod placement.

## Design Details
The core of this proposal is to implement an `ImageCompatibilityPlugin` within the Kubernetes scheduler framework, working with a new `ImageCompatibilityQuery` (ICQ) CRD and existing `NodeFeatureGroup` (NFG) CRD.

**Component Responsibilities:**
- **Mutating Webhook**: Parses OCI artifacts during Pod admission, creates ICQ CRs with `spec.compatibilityRules` only (no status computation).
- **Scheduler Plugin**: Computes and updates `status.compatibleNodes` for ICQs, performs PreBind validation, and detects post-scheduling drift.
- **nfd-master**: Updates `NodeFeatureGroup` status for admin-defined pre-groups only. Does not manage ICQ status.

### Proposal C: Node Pre-grouping

![compatibility_scheduler-proposal-C](./proposal-C.png)

For large scale clusters, node pre-grouping is a method to significantly reduce computational overhead. The core idea is to pre-organize all nodes into several groups based on specific, static rules (e.g., `cpu.model`, `kernel.version`) using `NodeFeatureGroup`. This optimization changes the scheduling complexity from checking **N (number of nodes)** down to just **G** groups (**G<<N**) in the critical path.

**New CRD: ImageCompatibilityQuery (ICQ)**

A new CRD `ImageCompatibilityQuery` is introduced to represent per-image compatibility queries. Unlike `NodeFeatureGroup` which groups nodes, ICQ represents the compatibility requirements of a specific image.

```yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: ImageCompatibilityQuery
metadata:
  name: icq-sha256-aaa123      # name = "icq-" + image digest prefix
  annotations:
    nfd.k8s-sigs.io/image-ref: "registry.example.com/app@sha256:aaa..."
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

The process involves these main phases:

1. **Initial Cluster Grouping:** In the cluster preparation stage, administrator should divide the cluster nodes into several groups by `NodeFeatureGroup`. Multiple `NodeFeatureGroup` CRs are created declaratively, each defining a grouping rule. Their status is populated with all matching nodes by nfd-master, completing the pre-grouping setup.
2. **Pod Admission (Webhook):** During Pod creation, the mutating webhook:
   - Fetches the OCI Artifact for each container image.
   - Extracts compatibility metadata.
   - Creates `ImageCompatibilityQuery` CRs with `spec.compatibilityRules` populated (status is not computed by webhook).
   - Annotates the Pod with image digests: `nfd.k8s-sigs.io/image-digests: "sha256:aaa,sha256:bbb"`.
3. **Scheduling Prefilter Phase:** The scheduler plugin:
   - Reads Pod annotations to get image digests.
   - For each image, checks if ICQ exists and has `status.compatibleNodes` ready.
   - If ICQ status is not ready, computes it by evaluating compatibility against admin pre-groups:
     - For each `NodeFeatureGroup`, selects one representative node and checks if it satisfies the ICQ's `spec.compatibilityRules`.
     - If the representative node matches, all nodes in that pre-group are added to `status.compatibleNodes`.
     - If the representative node does not match, the entire group is skipped.
   - Updates `status.compatibleNodes` and sets `conditions[Ready]=True`.
4. **Scheduling Filter Phase:** The scheduler filters candidate nodes by checking their presence in the `status.compatibleNodes` of all relevant ICQs (intersection for multi-image Pods).
5. **Scheduling PreBind Phase:** A final validation step that re-verifies node compatibility using the latest node features from informer cache. This catches any race conditions where ICQ status might be stale due to delayed informer updates. If validation fails, the binding is rejected and the pod is rescheduled.

**Multi-Image Pod Handling:**

For Pods with multiple containers (app + init + sidecars), each image gets its own ICQ. The scheduler computes the intersection of all ICQs' `status.compatibleNodes` during the Filter phase. Images without compatibility metadata are skipped (no ICQ created).

**Example Flow:** Assume 10000 nodes are pre-grouped into 10 groups (`Group-1` to `Group-10`) via `NodeFeatureGroup`. For a pod with a new compatibility demand, the webhook creates `ImageCompatibilityQuery-Compat-X` with spec only. The scheduler plugin evaluates the pre-groups using representative node matching. If `Group-1`'s representative node matches, all nodes from `Group-1` are added to `status.compatibleNodes`. This approach reduces the number of compatibility evaluations from **10,000 individual node checks** to **at most 10 representative node checks**.

**Key Characteristics:**

- **Administrator-Driven Grouping:** Node groups are statically predefined by the cluster administrator using `NodeFeatureGroup` in cluster preparation phase.
- **Representative Node Matching:** The core performance optimization is achieved by evaluating only a **single representative node** from each pre-existing group against the ICQ's compatibility rules, rather than scanning all nodes.
- **Scheduler Plugin Manages ICQ:** The scheduler plugin computes and updates `status.compatibleNodes` for ICQs, ensuring tight integration with the scheduling lifecycle.
- **PreBind Validation:** The PreBind phase provides real-time validation using the latest node features, catching race conditions where ICQ status might be stale.
- **ICQ Lifecycle Management:** ICQs are named by image digest prefix, enabling deduplication (1000 replicas of the same image → 1 ICQ). Reference counting and TTL-based GC manage lifecycle.

**Exception Handling:**
- If the OCI Artifact is unreachable or lacks compatibility metadata, the webhook skips ICQ creation for that image, and the plugin defaults to allowing scheduling on any node for that image. A warning is logged for visibility.
- If no pre-group's representative node matches the compatibility demands, the plugin correctly concludes that no compatible nodes exist in the cluster, resulting in a scheduling failure for the pod with logging an error.
- If a pre-group is found to be empty (i.e., its `status.nodes` list is empty), the plugin skips that group during evaluation, ensuring that only valid groups are considered.
- If PreBind validation fails (node drifted during scheduling), the binding is rejected and the pod is rescheduled to a compatible node.

**Advantages**

- **Significant Reduction in Computational Cost:** Shifts the complexity in the scheduling critical path from `O(N)` to `O(G)` (G is the group number, G<<N), delivering orders-of-magnitude performance improvement.
- **Aligns with Common Large Scale Cluster Practice:** Node grouping is common in large scale cluster, where administrators define multiple `NodeFeatureGroup` resources and assign nodes to these groups in advance.

**Limitations**

- **Small Modification to NodeFeatureGroup Operations**: Including a small amount of the `NodeFeatureGroup` operation modification.
- **Dependency on Group Homogeneity:** Requires administrator management for homogeneous grouping.

### Test Plan

To ensure the proper functioning of the compatibility scheduler plugin, the following test plan should be executed:

- **Unit Tests:** Write unit tests covering core logic for the plugin.
- **Manual e2e Tests:** Validate core end-to-end functionality by deploying a sample pod with compatibility artifacts. Including:
    - The ImageCompatibilityQuery is created correctly and the pod is successfully routed to a compatible node.
    - Spec-Hash deduplication, reference counting and TTL lazy deletion apply correctly.
    - PreBind validation catches stale ICQ status and rejects binding to drifted nodes.
    - Post-scheduling drift detection labels affected pods and generates events.
    - The default compatibility failure policy triggers.
- **Fault-Injection Tests:** Explicitly validate system resilience and fallback safety under the following failure modes:
    - **Registry Service Outage / Downtime:** Verify that when metadata becomes unreachable, setting failurePolicy: Fail correctly suspends/rejects the Pod, while setting failurePolicy: Ignore smoothly downgrades the safety policy without stalling the scheduling queue.
    - **NFD-Master Restart / Crash:** Verify that while the Master is offline, the scheduler can continuously make safe decisions using cached NodeFeatureGroup states, and validate that state synchronization latency remains minimal once the Master recovers.
    - **Stale ICQ Status:** Artificially inject a delay in ICQ status computation to verify that PreBind validation catches the staleness and rejects binding.
    - **Node Feature Drift During Scheduling:** Simulate a node feature change between Prefilter and PreBind to verify that PreBind validation rejects the drifted node.
- **Performance Tests:** Measure scheduling latency and ICQ update overhead under simulated heavy loads using Kwok (at 1k, 5k, and 10k nodes).
    - **Performance Targets:**

| Cluster Size (Nodes) | P99 Prefilter Latency | P99 Scheduling Latency | Success Rate at 50 Pods/s Arrival Rate |
| :--- | :--- | :--- | :--- | 
| **1k** | < 50ms | < 100ms | 100% |
| **5k** | < 100ms | < 200ms | 99.9% | 
| **10k** | < 250ms | < 500ms | 99% | 

### Graduation Criteria

#### Alpha
- Core Functionality Implementation: Complete the core development of the image compatibility scheduling plugin and NFG features.
- Basic Verification: Complete full E2E testing in a 100-node cluster to ensure all features are fully operational.
#### Beta
- Scalability Simulation: Complete simulation verification on a 5,000-node cluster, ensuring P99 Prefilter Latency < 100ms.
- Fault Tolerance: Validate system recovery capabilities under abnormal scenarios (e.g., Registry latency/downtime, NFD-Master restarts), achieving a success rate greater than 99.9%.
#### GA
- Extreme Performance & Production Verification: Complete long-term stability testing at a scale of 10,000 nodes.
- Production Adoption: Gather deployment cases and performance feedback reports under real-world workloads from at least 2 independent production environments.

## Implementation History
- 2025-12-27: KEP proposal submission
- 2026-01-20: Update KEP with proposal C(Node pre-grouping) chosen as preferred solution.
- 2026-06-15: Update KEP with refined architecture:
  - Introduce `ImageCompatibilityQuery` CRD (separate from `NodeFeatureGroup`)
  - Clarify component responsibilities: Webhook creates ICQ spec, Scheduler Plugin computes status, nfd-master updates NFG only
  - Add PreBind validation for real-time drift detection
  - Add post-scheduling drift detection with label + log + event alerting
  - Remove explicit `Homogeneous` field; use internal homogeneity detection
## Alternatives Considered

### Use Node Affinity/Node Selector Directly in Pod Spec
Using standard Kubernetes features like Node Affinity or Node Selector directly in the Pod specification to specify compatibility requirements was considered. However, this approach has several limitations:
- Not all node features are reflected in labels.
- Compatibility rules are typically structured, multi-dimensional, and frequently updated. Node affinity/Node selector, in essence, is a static label-matching system and is unsuitable to handle such dynamic and complex requirements.
- In large scale clusters, node affinity/node selector does not perform well in terms of time consumption.

### Alternative design proposals
#### Proposal A: NodeFeatureGroup Check

![compatibility_scheduler-proposal-A](./proposal-A.png)

The basic solution is a direct, on-demand approach by utilizing the `NodeFeatureGroup` Custom Resource (CR) to dynamically define and manage node compatibility groups at scheduling time.

**Workflow:**

1. **CR Creation and Update (Prefilter Phase):** When a pod with specific image requirements enters the scheduling queue, the scheduler plugin fetches the attached OCI Artifact. It extracts the compatibility metadata (e.g., required kernel features) and **instantly creates a new `NodeFeatureGroup` CR**. This CR's specification defines the dynamic compatibility rules.

   The `update NodeFeatureGroup` operation evaluates **all nodes in the cluster** against the CR's specification rules and updates the CR's `status` field with the list of nodes that satisfy the compatibility demands.

   ```yaml
   apiVersion: nfd.k8s-sigs.io/v1alpha1
   kind: NodeFeatureGroup
   metadata:
     name: node-feature-group-example
   spec:
     featureGroupRules:
       - name: "kernel version"
         matchFeatures:
           - feature: kernel.version
             matchExpressions:
               major: {op: In, value: ["6"]}
   status:
     nodes:
       - name: node-1
       - name: node-2
       - name: node-3
   ```

2. **Node Filtering (Filter Phase):** In the scheduler's final filter phase, retrieve the dynamically created `NodeFeatureGroup` CR and filters the candidate nodes, ensuring that only nodes listed in the CR's `status` are considered compatible.

**Advantages**

- **Simplicity:** Filter candidate nodes by compatible nodes set from `NodeFeatureGroup`.
- **Non-Invasive:** Without modifications to existing `NodeFeatureGroup` operation.

**Limitations**

- **Performance Limitation:** The requirement to evaluate **all cluster nodes** for each scheduling request creates a linear scalability bottleneck (`O(N)` complexity). In a large scale cluster (e.g., 65,000 nodes), this introduces significant latency in the scheduling critical path.
- **No Caching:** Repeated scheduling for similar image requirements results in redundant node evaluation work, as no intermediate results are cached or reused.

#### Proposal B: SQLite Database Caching for Node Features in Large Scale Clusters

![compatibility_scheduler-proposal-B](./proposal-B.png)

NodeFeatureGroup(NFG) updates status by computing all nodes' features. It can become performance bottleneck in a large scale cluster. The problem is conceptually similar to **map-reduce**, where data must be aggregated before processing.

To achieve this, we'll need an additional controller that watches nodes, collects reports from workers, and maintains a cache database with grouped nodes. NFG would then act on this cached, grouped data instead of raw per node inputs. Fast lookups will depend on an efficient cache data structure.

**Cache Implement** 

- **Implementation**: The NFD master is extended to include an **embedded SQLite database**, which functions as a high-performance local sink. It continuously aggregates and stores feature reports (e.g., CPU flags, kernel versions, PCI devices) collected from NFD worker daemons across all nodes in the cluster.
- **Data Model:** The database adopts an indexed **Entity-Attribute-Value (EAV) schema**. Each record explicitly links a node (the Entity) with a specific feature name (the Attribute) and its current state (the Value), enabling complex, multi-dimensional queries for node grouping.
- **Persistence:** To ensure data durability, the enhanced NFD master pod is configured with a **PVC (~100 GB)**. For optimal I/O performance critical to low-latency queries, the use of high-performance storage solutions (**Longhorn** or **local storage**) is recommended.

**NFG Update**

- **Fast Query**: When a NFG is deployed, the master can run a fast local sql query to determine which nodes match the group's conditions (e.g. `cpu.avx2=true`, `kernel.version>=6.6`, `pci.vendor=10de`). 
- **Pre Load**: Any image artifacts related NFG could be fetched asynchronously before scheduling (e.g., by an admission webhook or a small controller). The sqlite database is used only as a **precomputation engine** and not contained within the scheduling process. The NFG update process can be completed before scheduler process. The scheduler plugin would then watch those results in memory and perform constant time membership checks during scheduling. This approach keeps the scheduler's filter phase pure and nonblocking, avoiding disk or network I/O inside the hot path. 

**Scheduler Process**

- **Fetch results from NFG**: The scheduling workflow remains conceptually consistent with the basic solution (Solution 1). The critical difference is that all computationally expensive operations have been shifted to the asynchronous precomputation stage. The scheduler plugin just retrieves the precomputed results from the NFG status to identify the compatible nodes group.

**Advantages**

- **High Performance with Fast Queries:** By leveraging an indexed SQLite database with an EAV schema, node feature queries are executed with high efficiency. 
- **Non-blocking Scheduler Filter Phase:** All node evaluation and `NodeFeatureGroup` status calculations are completed asynchronously.

**Limitations**

- **Modification to NodeFeatureGroup Operations**
- **More work with NFG fetch webhook/controller:** This might become a new feature or a standalone solution.
