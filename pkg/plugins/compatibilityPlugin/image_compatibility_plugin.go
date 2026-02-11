package compatibilityPlugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
	"oras.land/oras-go/v2/registry"
	nfdclientset "sigs.k8s.io/node-feature-discovery/api/generated/clientset/versioned"
	nfdv1alpha1 "sigs.k8s.io/node-feature-discovery/api/nfd/v1alpha1"
	"sigs.k8s.io/node-feature-discovery/pkg/apis/nfd/nodefeaturerule"
	artifactcli "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/artifact-client"
	"sigs.k8s.io/node-feature-discovery/pkg/utils"
)

// New creates a new ImageCompatibilityPlugin instance.
func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	// Parse plugin configuration arguments
	args := ImageCompatibilityPluginArgs{}
	if configuration != nil {
		// Convert runtime.Object to the plugin args type
		configBytes, err := json.Marshal(configuration)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal plugin configuration: %w", err)
		}
		if err := json.Unmarshal(configBytes, &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal plugin configuration: %w", err)
		}
	}

	// Initialize NFD client for accessing NodeFeatureGroup CRs.
	var (
		nfdCli nfdclientset.Interface
	)

	// Scheduler usually runs in-cluster as a Pod, so use InClusterConfig.
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		klog.Errorf("failed to create in-cluster config for nfd client: %v", err)
	} else {
		if cli, err := nfdclientset.NewForConfig(restCfg); err != nil {
			klog.Errorf("failed to create nfd clientset: %v", err)
		} else {
			nfdCli = cli
		}
	}

	// Dynamically discover nfd-master namespace
	nfdMasterNamespace, err := discoverNfdMasterNamespace(ctx, handle.ClientSet())
	if err != nil {
		klog.Errorf("failed to discover nfd-master namespace: %v, will retry on first use", err)
		// Continue with empty namespace, will be discovered lazily
	}

	// Determine custom scheduler namespace
	customSchedulerNamespace := args.Namespace
	if customSchedulerNamespace == "" {
		customSchedulerNamespace = "custom-scheduler"
	}

	return &ImageCompatibilityPlugin{
		handle:             handle,
		nfdClient:          nfdCli,
		nfdMasterNamespace: nfdMasterNamespace,
		namespace:          customSchedulerNamespace,
		args:               args,
		imageToNFGCache:    make(map[string][]string),
	}, nil
}

// discoverNfdMasterNamespace finds the namespace where nfd-master is running
// by searching for pods with the nfd-master label selector.
func discoverNfdMasterNamespace(ctx context.Context, clientSet k8sclient.Interface) (string, error) {
	// Search in common namespaces first
	namespaces := []string{"node-feature-discovery", "kube-system", "default"}

	for _, ns := range namespaces {
		pods, err := clientSet.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: NfdMasterLabelSelector,
		})
		if err != nil {
			continue
		}
		if len(pods.Items) > 0 {
			return ns, nil
		}
	}

	// If not found in common namespaces, search all namespaces
	pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: NfdMasterLabelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}
	// If not found, return empty namespace with log
	if len(pods.Items) == 0 {
		klog.Warningf("nfd-master pod not found with label selector %s", NfdMasterLabelSelector)
		return "", nil
	}

	return pods.Items[0].Namespace, nil
}

// Name returns the plugin name.
func (f *ImageCompatibilityPlugin) Name() string {
	return PluginName
}

// PreFilter is invoked at the PreFilter extension point.
func (f *ImageCompatibilityPlugin) PreFilter(ctx context.Context, cycleState fwk.CycleState, pod *v1.Pod, filteredNodes []fwk.NodeInfo) (*framework.PreFilterResult, *fwk.Status) {
	// Ensure nfd-master namespace is discovered
	nfdMasterNamespace, err := f.getNfdMasterNamespace(ctx)
	if err != nil {
		return nil, fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to get nfd-master namespace: %v", err))
	}

	// Create NodeFeatureGroup CRs for all container images in custom-scheduler namespace
	// Store created NFG names in cycle state for later reference
	createdNFGs, err := f.createNodeFeatureGroupsForPod(ctx, pod, f.namespace)
	if err != nil {
		return nil, fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to create NodeFeatureGroups: %v", err))
	}

	// Update compatibility NFGs with nodes from pre-created group NFGs
	// This is needed because nfd won't auto-update NFGs in custom-scheduler namespace
	err = f.updateCompatibilityNFGsWithPreGroups(ctx, nfdMasterNamespace, f.namespace, createdNFGs)
	if err != nil {
		klog.Errorf("Failed to update compatibility NFGs with pre-created groups: %v", err)
		// Continue even if update fails
	}

	// Collect compatible nodes from updated compatibility NFGs
	compatibleNodes, err := f.collectCompatibleNodesFromNFGs(ctx, f.namespace, createdNFGs)
	if err != nil {
		return nil, fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to collect compatible nodes from NFGs: %v", err))
	}

	// Store NFG names and compatible nodes in cycle state for Filter phase
	state := &CompatibilityState{
		CompatibleNodes: compatibleNodes,
		CreatedNFGs:     createdNFGs,
		Namespace:       f.namespace,
	}
	cycleState.Write(PluginName, state)

	return nil, fwk.NewStatus(fwk.Success)
}

// PreFilterExtensions returns prefilter extensions, pod add and remove.
func (f *ImageCompatibilityPlugin) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Filter is invoked at the Filter extension point and rejects nodes
// that are not present in the compatible node snapshot stored in the
// scheduling cycle state.
func (f *ImageCompatibilityPlugin) Filter(ctx context.Context, cycleState fwk.CycleState, pod *v1.Pod, nodeInfo fwk.NodeInfo) *fwk.Status {
	node := nodeInfo.Node()
	if node == nil {
		klog.Errorf("NodeInfo for pod %s is nil", pod.Name)
		return fwk.NewStatus(fwk.Error, "node not found")
	}

	// Get compatibility state from cycle state
	state, err := getCompatibilityState(cycleState)
	if err != nil {
		klog.Errorf("failed to get compatibility state for pod %s: %v", pod.Name, err)
		return fwk.NewStatus(fwk.Error, fmt.Sprintf("get compatibility state error: %v", err))
	}

	// If no compatible nodes found, reject the node
	if len(state.CompatibleNodes) == 0 {
		klog.Warningf("No compatible nodes found for pod %s", pod.Name)
		return fwk.NewStatus(
			fwk.Unschedulable,
			fmt.Sprintf("node %s is not compatible with pod images", node.Name),
		)
	}

	// Check if current node is compatible
	if _, ok := state.CompatibleNodes[node.Name]; !ok {
		return fwk.NewStatus(
			fwk.Unschedulable,
			fmt.Sprintf("node %s is not listed in any compatible NodeFeatureGroup status", node.Name),
		)
	}

	return fwk.NewStatus(fwk.Success)
}

// getCompatibilityState reads CompatibilityState from CycleState.
func getCompatibilityState(cycleState fwk.CycleState) (*CompatibilityState, error) {
	data, err := cycleState.Read(PluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to read cycle state for plugin %s: %w", PluginName, err)
	}

	state, ok := data.(*CompatibilityState)
	if !ok {
		return nil, fmt.Errorf("unexpected state type %T for plugin %s", data, PluginName)
	}

	return state, nil
}

// getNfdMasterNamespace returns the nfd-master namespace, discovering it if needed.
func (f *ImageCompatibilityPlugin) getNfdMasterNamespace(ctx context.Context) (string, error) {
	if f.nfdMasterNamespace != "" {
		return f.nfdMasterNamespace, nil
	}

	// Lazy discovery if not set during initialization
	namespace, err := discoverNfdMasterNamespace(ctx, f.handle.ClientSet())
	if err != nil {
		return "", err
	}

	f.nfdMasterNamespace = namespace
	return namespace, nil
}

// createNodeFeatureGroupsForPod creates temporary NodeFeatureGroup CRs for all
// container images declared in the Pod spec. These CRs will be automatically
// cleaned up when the Pod is deleted via OwnerReference TTL mechanism.
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForPod(ctx context.Context, pod *v1.Pod, namespace string) ([]string, error) {
	var createdNFGs []string
	for _, container := range pod.Spec.Containers {
		nfgNames, err := f.createNodeFeatureGroupsForImage(ctx, pod, container.Image, namespace)
		if err != nil {
			return nil, fmt.Errorf("create NodeFeatureGroups for image %s failed: %w", container.Image, err)
		}
		createdNFGs = append(createdNFGs, nfgNames...)
	}
	return createdNFGs, nil
}

// updateCacheForImage updates the cache for a specific image with the given NFG names
func (f *ImageCompatibilityPlugin) updateCacheForImage(imageName string, nfgNames []string) {
	if len(nfgNames) == 0 {
		return
	}

	f.imageToNFGCacheMutex.Lock()
	f.imageToNFGCache[imageName] = nfgNames
	f.imageToNFGCacheMutex.Unlock()
	klog.Infof("Cached NFGs %v for image %s", nfgNames, imageName)
}

// removeFromCache removes an image from the cache
func (f *ImageCompatibilityPlugin) removeFromCache(imageName string) {
	f.imageToNFGCacheMutex.Lock()
	delete(f.imageToNFGCache, imageName)
	f.imageToNFGCacheMutex.Unlock()
	klog.Infof("Removed image %s from cache", imageName)
}

// getValidCachedNFGs returns valid NFGs from cache for a specific image
func (f *ImageCompatibilityPlugin) getValidCachedNFGs(ctx context.Context, imageName, namespace string) ([]string, bool) {
	f.imageToNFGCacheMutex.RLock()
	cachedNFGs, found := f.imageToNFGCache[imageName]
	f.imageToNFGCacheMutex.RUnlock()

	if !found || len(cachedNFGs) == 0 {
		return nil, false
	}

	// Verify all cached NFGs still exist
	validNFGs := []string{}
	for _, nfgName := range cachedNFGs {
		if _, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(namespace).Get(ctx, nfgName, metav1.GetOptions{}); err == nil {
			validNFGs = append(validNFGs, nfgName)
		}
	}

	if len(validNFGs) == 0 {
		// All cached NFGs are invalid
		f.removeFromCache(imageName)
		return nil, false
	}

	// Update cache with only valid NFGs if some were invalid
	if len(validNFGs) != len(cachedNFGs) {
		f.updateCacheForImage(imageName, validNFGs)
		klog.Infof("Updated cache for image %s: removed %d invalid NFGs", imageName, len(cachedNFGs)-len(validNFGs))
	}

	return validNFGs, true
}

// createNodeFeatureGroupsForImage creates NodeFeatureGroup CRs for a
// single image artifact with TTL via OwnerReference to the Pod.
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForImage(ctx context.Context, pod *v1.Pod, imageName, namespace string) ([]string, error) {
	// Check cache first
	if validNFGs, found := f.getValidCachedNFGs(ctx, imageName, namespace); found {
		klog.Infof("Reusing cached NFGs %v for image %s", validNFGs, imageName)
		return validNFGs, nil
	}

	ref, err := registry.ParseReference(imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
	}

	ac := artifactcli.New(
		&ref,
		artifactcli.WithArgs(artifactcli.Args{PlainHttp: f.args.PlainHttp}),
		artifactcli.WithAuthDefault(),
	)

	mgmt := NewFeatureGroupManagement(ac)
	nfgs, err := mgmt.CreateNodeFeatureGroupsFromArtifact(ctx, f.nfdClient, pod, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create NodeFeatureGroups from artifact for image %s: %w", imageName, err)
	}

	// Extract NFG names and update cache
	var nfgNames []string
	for _, nfg := range nfgs {
		nfgNames = append(nfgNames, nfg.Name)
	}

	// Update cache with all NFG names
	f.updateCacheForImage(imageName, nfgNames)

	return nfgNames, nil
}

// collectCompatibleNodesFromNFGs computes compatible nodes from specific NFGs with retry logic
func (f *ImageCompatibilityPlugin) collectCompatibleNodesFromNFGs(ctx context.Context, namespace string, nfgNames []string) (map[string]struct{}, error) {
	startTime := time.Now()
	maxWait := NfdUpdateGracePeriod
	retryInterval := 500 * time.Millisecond

	for attempt := 0; time.Since(startTime) < maxWait; attempt++ {
		intersection := f.computeIntersection(ctx, namespace, nfgNames)

		if len(intersection) > 0 {
			klog.Infof("Found %d compatible nodes after %v", len(intersection), time.Since(startTime))
			return intersection, nil
		}

		// Wait and retry
		if time.Since(startTime) < maxWait {
			klog.Infof("No compatible nodes, waiting (attempt %d, elapsed: %v)", attempt+1, time.Since(startTime))
			time.Sleep(retryInterval)
		}
	}

	klog.Warningf("No compatible nodes found after waiting %v", maxWait)
	return make(map[string]struct{}), nil
}

// computeIntersection computes intersection of nodes from all NFGs
func (f *ImageCompatibilityPlugin) computeIntersection(ctx context.Context, namespace string, nfgNames []string) map[string]struct{} {
	var intersection map[string]struct{}
	first := true

	for _, nfgName := range nfgNames {
		nodes, err := f.getNFGNodes(ctx, namespace, nfgName)
		if err != nil || len(nodes) == 0 {
			continue
		}

		if first {
			intersection = nodes
			first = false
			continue
		}

		// Intersect with current nodes
		for node := range intersection {
			if _, ok := nodes[node]; !ok {
				delete(intersection, node)
			}
		}
	}

	if intersection == nil {
		return make(map[string]struct{})
	}
	return intersection
}

// getNFGNodes retrieves nodes from a specific NFG
func (f *ImageCompatibilityPlugin) getNFGNodes(ctx context.Context, namespace, nfgName string) (map[string]struct{}, error) {
	nfg, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(namespace).Get(ctx, nfgName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]struct{})
	for _, n := range nfg.Status.Nodes {
		nodes[n.Name] = struct{}{}
	}
	return nodes, nil
}

// updateCompatibilityNFGsWithPreGroups updates compatibility NFGs with nodes from pre-created group NFGs
// This method needs to call nfd-master's matching logic since compatibility NFGs won't auto-update status
func (f *ImageCompatibilityPlugin) updateCompatibilityNFGsWithPreGroups(ctx context.Context, preNamespace, compatibilityNamespace string, compatibilityNFGNames []string) error {
	// TODO: Implement nfd-master matching logic
	// For now, just log that we would update the NFGs
	klog.Infof("Would update compatibility NFGs %v in namespace %s with pre-created groups from namespace %s",
		compatibilityNFGNames, compatibilityNamespace, preNamespace)

	// Get all pre-created group NFGs
	preNFGs, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(preNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pre-created group NFGs: %w", err)
	}

	if len(preNFGs.Items) == 0 {
		klog.Infof("No pre-created group NFGs found in namespace %s", preNamespace)
		return nil
	}

	klog.Infof("Found %d pre-created group NFGs in namespace %s", len(preNFGs.Items), preNamespace)

	// For now, we just log the matching logic would be implemented
	// Actual implementation would need to:
	// 1. Call nfd-master API to match nodes between pre-created NFGs and compatibility NFGs
	// 2. Update compatibility NFG status with matched nodes from pre-created groups
	for _, preNFG := range preNFGs.Items {
		nfg, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(preNamespace).Get(ctx, preNFG.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get pre-created NFG %s: %v", preNFG.Name, err)
			continue
		}
		if nfg.Status.Nodes == nil || len(nfg.Status.Nodes) == 0 {
			klog.Warningf("Pre-created NFG %s has no nodes in status", preNFG.Name)
			continue
		}

		// Check if any node in the preNFG matches the rules
		matchedNodes := make([]nfdv1alpha1.FeatureGroupNode, 0)
		for _, preNode := range nfg.Status.Nodes {
			nodeFeature, err := f.getAndMergeNodeFeatures(ctx, preNode.Name)
			if err != nil {
				klog.Errorf("Failed to get NodeFeatures for node %s: %v", preNode.Name, err)
				continue
			}
			features := &nodeFeature.Spec.Features

			// Execute rules for this node
			ruleMatched := false
			for _, rule := range nfg.Spec.Rules {
				ruleOut, err := nodefeaturerule.ExecuteGroupRule(&rule, features, true)
				if err != nil {
					klog.Errorf("Failed to execute rule %s: %v", rule.Name, err)
					continue
				}
				if ruleOut.MatchStatus.IsMatch {
					ruleMatched = true
					break
				}
				// Feed back vars from rule output to features map for subsequent rules to match
				features.InsertAttributeFeatures(nfdv1alpha1.RuleBackrefDomain, nfdv1alpha1.RuleBackrefFeature, ruleOut.Vars)
			}

			// If any rule matched, add this node to matched nodes
			if ruleMatched {
				matchedNodes = append(matchedNodes, nfdv1alpha1.FeatureGroupNode{
					Name: preNode.Name,
				})
			}
		}

		// If we have matched nodes, update compatibility NFGs
		if len(matchedNodes) > 0 {
			klog.Infof("Pre-created NFG %s has %d nodes matching rules, would update compatibility NFGs",
				preNFG.Name, len(matchedNodes))
			// TODO: Actually update compatibility NFG status with matchedNodes
		} else {
			klog.Infof("Pre-created NFG %s has no nodes matching rules", preNFG.Name)
		}
	}

	return nil
}

// getAndMergeNodeFeatures merges the NodeFeature objects of the given node into a single NodeFeatureSpec.
// The Name field of the returned NodeFeatureSpec contains the node name.
// This is a simplified version that uses the Kubernetes API directly instead of the internal nfdmaster featureLister.
func (f *ImageCompatibilityPlugin) getAndMergeNodeFeatures(ctx context.Context, nodeName string) (*nfdv1alpha1.NodeFeature, error) {
	nodeFeatures := &nfdv1alpha1.NodeFeature{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	// If nfdClient is not available, return empty NodeFeature
	if f.nfdClient == nil {
		return &nfdv1alpha1.NodeFeature{}, nil
	}

	// List all NodeFeature objects across all namespaces with the node name label
	labelSelector := fmt.Sprintf("%s=%s", nfdv1alpha1.NodeFeatureObjNodeNameLabel, nodeName)
	allNodeFeatures, err := f.nfdClient.NfdV1alpha1().NodeFeatures("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return &nfdv1alpha1.NodeFeature{}, fmt.Errorf("failed to get NodeFeature resources for node %q: %w", nodeName, err)
	}

	filteredObjs := []*nfdv1alpha1.NodeFeature{}
	for i := range allNodeFeatures.Items {
		// For simplicity, we accept all namespaces
		// In the original NFD implementation, there's namespace filtering logic
		filteredObjs = append(filteredObjs, &allNodeFeatures.Items[i])
	}

	// Node without a running NFD-Worker
	if len(filteredObjs) == 0 {
		return &nfdv1alpha1.NodeFeature{}, nil
	}

	// Sort our objects
	sort.Slice(filteredObjs, func(i, j int) bool {
		// Objects in our nfd namespace gets into the beginning of the list
		if filteredObjs[i].Namespace == f.namespace && filteredObjs[j].Namespace != f.namespace {
			return true
		}
		if filteredObjs[i].Namespace != f.namespace && filteredObjs[j].Namespace == f.namespace {
			return false
		}
		// After the nfd namespace, sort objects by their name
		if filteredObjs[i].Name != filteredObjs[j].Name {
			return filteredObjs[i].Name < filteredObjs[j].Name
		}
		// Objects with the same name are sorted by their namespace
		return filteredObjs[i].Namespace < filteredObjs[j].Namespace
	})

	if len(filteredObjs) > 0 {
		// Merge in features
		//
		// NOTE: changing the rule api to support handle multiple objects instead
		// of merging would probably perform better with lot less data to copy.
		features := filteredObjs[0].Spec.DeepCopy()

		// Simplified: Skip the complex NFD configuration checks
		// In the original code, there are checks for DenyNodeFeatureLabels and NFDFeatureGate
		// For our use case, we assume these features are enabled/disabled as needed

		for _, o := range filteredObjs[1:] {
			s := o.Spec.DeepCopy()
			s.MergeInto(features)
		}

		// Set the merged features to the NodeFeature object
		nodeFeatures.Spec = *features

		klog.V(4).InfoS("merged nodeFeatureSpecs", "newNodeFeatureSpec", utils.DelayedDumper(features))
	}

	return nodeFeatures, nil
}
