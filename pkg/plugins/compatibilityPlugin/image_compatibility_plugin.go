package compatibilityPlugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	fwk "k8s.io/kube-scheduler/framework"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
	"oras.land/oras-go/v2/registry"
	nfdclientset "sigs.k8s.io/node-feature-discovery/api/generated/clientset/versioned"
	artifactcli "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/artifact-client"
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
		log.Printf("failed to create in-cluster config for nfd client: %v", err)
	} else {
		if cli, err := nfdclientset.NewForConfig(restCfg); err != nil {
			log.Printf("failed to create nfd clientset: %v", err)
		} else {
			nfdCli = cli
		}
	}

	// Dynamically discover nfd-master namespace
	nfdMasterNamespace, err := discoverNfdMasterNamespace(ctx, handle.ClientSet())
	if err != nil {
		log.Printf("failed to discover nfd-master namespace: %v, will retry on first use", err)
		// Continue with empty namespace, will be discovered lazily
	}

	return &ImageCompatibilityPlugin{
		handle:             handle,
		nfdClient:          nfdCli,
		nfdMasterNamespace: nfdMasterNamespace,
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
		log.Printf("nfd-master pod not found with label selector %s", NfdMasterLabelSelector)
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
	namespace, err := f.getNfdMasterNamespace(ctx)
	if err != nil {
		return nil, fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to get nfd-master namespace: %v", err))
	}

	// Create NodeFeatureGroup CRs for all container images
	// Store created NFG names in cycle state for later reference
	createdNFGs, err := f.createNodeFeatureGroupsForPod(ctx, pod, namespace)
	if err != nil {
		return nil, fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to create NodeFeatureGroups: %v", err))
	}

	// Store NFG names in cycle state for Filter phase
	state := &CompatibilityState{
		CompatibleNodes: make(map[string]struct{}),
		CreatedNFGs:     createdNFGs,
		Namespace:       namespace,
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
		log.Printf("NodeInfo for pod %s is nil", pod.Name)
		return fwk.NewStatus(fwk.Error, "node not found")
	}

	// Get compatibility state from cycle state
	state, err := getCompatibilityState(cycleState)
	if err != nil {
		log.Printf("failed to get compatibility state for pod %s: %v", pod.Name, err)
		return fwk.NewStatus(fwk.Error, fmt.Sprintf("get compatibility state error: %v", err))
	}

	// If compatible nodes haven't been computed yet, compute them now
	if len(state.CompatibleNodes) == 0 && len(state.CreatedNFGs) > 0 {
		compatibleNodes, err := f.collectCompatibleNodesFromNFGs(ctx, state.Namespace, state.CreatedNFGs)
		if err != nil {
			return fwk.NewStatus(fwk.Error, fmt.Sprintf("failed to collect compatible nodes from NFGs: %v", err))
		}
		state.CompatibleNodes = compatibleNodes
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
	log.Printf("Cached NFGs %v for image %s", nfgNames, imageName)
}

// removeFromCache removes an image from the cache
func (f *ImageCompatibilityPlugin) removeFromCache(imageName string) {
	f.imageToNFGCacheMutex.Lock()
	delete(f.imageToNFGCache, imageName)
	f.imageToNFGCacheMutex.Unlock()
	log.Printf("Removed image %s from cache", imageName)
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
		log.Printf("Updated cache for image %s: removed %d invalid NFGs", imageName, len(cachedNFGs)-len(validNFGs))
	}

	return validNFGs, true
}

// createNodeFeatureGroupsForImage creates NodeFeatureGroup CRs for a
// single image artifact with TTL via OwnerReference to the Pod.
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForImage(ctx context.Context, pod *v1.Pod, imageName, namespace string) ([]string, error) {
	// Check cache first
	if validNFGs, found := f.getValidCachedNFGs(ctx, imageName, namespace); found {
		log.Printf("Reusing cached NFGs %v for image %s", validNFGs, imageName)
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

// collectCompatibleNodesFromNFGs computes compatible nodes from specific NFGs(take the intersection)
func (f *ImageCompatibilityPlugin) collectCompatibleNodesFromNFGs(ctx context.Context, namespace string, nfgNames []string) (map[string]struct{}, error) {
	var (
		intersection map[string]struct{}
		firstGroup   = true
	)

	for _, nfgName := range nfgNames {
		nfg, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(namespace).Get(ctx, nfgName, metav1.GetOptions{})
		if err != nil {
			// If NFG not found, skip it (might have been deleted)
			continue
		}

		currentGroup := make(map[string]struct{})
		for _, n := range nfg.Status.Nodes {
			currentGroup[n.Name] = struct{}{}
		}

		if firstGroup {
			intersection = currentGroup
			firstGroup = false
			continue
		}

		for name := range intersection {
			if _, ok := currentGroup[name]; !ok {
				delete(intersection, name)
			}
		}
	}

	if intersection == nil {
		intersection = make(map[string]struct{})
	}

	return intersection, nil
}
