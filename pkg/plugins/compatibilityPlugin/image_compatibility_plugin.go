package compatibilityPlugin

import (
	"context"
	"fmt"
	"log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	fwk "k8s.io/kube-scheduler/framework"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
	"oras.land/oras-go/v2/registry"
	nfdclientset "sigs.k8s.io/node-feature-discovery/api/generated/clientset/versioned"
	artifactcli "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/artifact-client"
	nfdmaster "sigs.k8s.io/node-feature-discovery/pkg/nfd-master"
)

// New creates a new ImageCompatibilityPlugin instance.
func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
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

	return &ImageCompatibilityPlugin{
		handle:     handle,
		jobManager: NewJobManager(handle.ClientSet(), JobNamespace),
		nfdClient:  nfdCli,
	}, nil
}

// Name returns the plugin name.
func (f *ImageCompatibilityPlugin) Name() string {
	return PluginName
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

	state, err := ensureCompatibilityState(ctx, f, cycleState, pod)
	if err != nil {
		log.Printf("failed to prepare compatibility state for pod %s: %v", pod.Name, err)
		return fwk.NewStatus(fwk.Error, fmt.Sprintf("prepare compatibility state error: %v", err))
	}

	if _, ok := state.CompatibleNodes[node.Name]; !ok {
		return fwk.NewStatus(
			fwk.Unschedulable,
			fmt.Sprintf("node %s is not listed in any compatible NodeFeatureGroup status", node.Name),
		)
	}

	return fwk.NewStatus(fwk.Success)
}

// ensureCompatibilityState ensures that the compatible node set for
// the given Pod has been computed and stored in CycleState.
func ensureCompatibilityState(ctx context.Context, f *ImageCompatibilityPlugin, cycleState fwk.CycleState, pod *v1.Pod) (*CompatibilityState, error) {
	if state, err := getCompatibilityState(cycleState); err == nil {
		return state, nil
	}

	compatibleNodes, err := f.buildCompatibleNodeSet(ctx, pod)
	if err != nil {
		return nil, err
	}

	state := &CompatibilityState{CompatibleNodes: compatibleNodes}
	cycleState.Write(PluginName, state)
	return state, nil
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

// buildCompatibleNodeSet computes the compatible node set based on
// all container images used by the Pod.
func (f *ImageCompatibilityPlugin) buildCompatibleNodeSet(ctx context.Context, pod *v1.Pod) (map[string]struct{}, error) {
	if err := f.createNodeFeatureGroupsForPod(ctx, pod); err != nil {
		return nil, err
	}

	if err := runNfdMasterOnce(); err != nil {
		return nil, err
	}

	return f.collectCompatibleNodes(ctx, NfdMasterNamespace)
}

// createNodeFeatureGroupsForPod creates temporary NodeFeatureGroup CRs for all
// container images declared in the Pod spec. These CRs will be automatically
// cleaned up when the Pod is deleted via OwnerReference TTL mechanism.
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForPod(ctx context.Context, pod *v1.Pod) error {
	for _, container := range pod.Spec.Containers {
		if err := f.createNodeFeatureGroupsForImage(ctx, pod, container.Image); err != nil {
			return fmt.Errorf("create NodeFeatureGroups for image %s failed: %w", container.Image, err)
		}
	}
	return nil
}

// createNodeFeatureGroupsForImage creates NodeFeatureGroup CRs for a
// single image artifact with TTL via OwnerReference to the Pod.
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForImage(ctx context.Context, pod *v1.Pod, imageName string) error {
	ref, err := registry.ParseReference(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
	}

	ac := artifactcli.New(
		&ref,
		artifactcli.WithArgs(artifactcli.Args{PlainHttp: false}),
		artifactcli.WithAuthDefault(),
	)

	mgmt := NewFeatureGroupManagement(ac)
	if _, err := mgmt.CreateNodeFeatureGroupsFromArtifact(ctx, f.nfdClient, pod); err != nil {
		return fmt.Errorf("failed to create NodeFeatureGroups from artifact for image %s: %w", imageName, err)
	}

	return nil
}

// runNfdMasterOnce runs nfd-master so that NodeFeatureGroup status
// objects are updated with matching nodes.
func runNfdMasterOnce() error {
	args := &nfdmaster.Args{}
	nfdMaster, err := nfdmaster.NewNfdMaster(nfdmaster.WithArgs(args))
	if err != nil {
		return fmt.Errorf("failed to create nfdMaster: %w", err)
	}

	if err := nfdMaster.Run(); err != nil {
		return fmt.Errorf("failed to run nfdMaster: %w", err)
	}

	return nil
}

// collectCompatibleNodes computes the intersection of node names
// across all NodeFeatureGroup status objects, yielding the set of
// nodes that are compatible with all referenced images.
func (f *ImageCompatibilityPlugin) collectCompatibleNodes(ctx context.Context, namespace string) (map[string]struct{}, error) {
	nfgList, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list NodeFeatureGroups: %w", err)
	}

	var (
		intersection map[string]struct{}
		firstGroup   = true
	)

	for _, nfg := range nfgList.Items {
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
