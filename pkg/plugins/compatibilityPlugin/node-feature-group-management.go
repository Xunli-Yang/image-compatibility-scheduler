package compatibilityPlugin

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	nfdclientset "sigs.k8s.io/node-feature-discovery/api/generated/clientset/versioned"
	nfdv1alpha1 "sigs.k8s.io/node-feature-discovery/api/nfd/v1alpha1"
	artifactcli "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/artifact-client"
)

type FeatureGroupManagement struct {
	artifactClient artifactcli.ArtifactClient
	k8sClient      k8sclient.Interface
	namespace      string
}

// NewFeatureGroupManagement creates a new FeatureGroupManagement instance
func NewFeatureGroupManagement(artifactClient artifactcli.ArtifactClient) *FeatureGroupManagement {
	return &FeatureGroupManagement{
		artifactClient: artifactClient,
	}
}

// CreateNodeFeatureGroupsFromArtifact creates temporary NodeFeatureGroup CRs based on
// compatibility spec in artifact. These CRs are owned by the Pod and will be automatically
// deleted when the Pod is deleted via Kubernetes garbage collection.
func (fgm *FeatureGroupManagement) CreateNodeFeatureGroupsFromArtifact(ctx context.Context, cli nfdclientset.Interface, pod *v1.Pod, namespace string) ([]nfdv1alpha1.NodeFeatureGroup, error) {
	nodeFeatureGroups, err := fgm.TransferFromArtifact(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to transfer from artifact: %v", err)
	}

	// Set up OwnerReference for automatic cleanup when Pod is deleted
	ownerRef := metav1.OwnerReference{
		APIVersion:         "v1",
		Kind:               "Pod",
		Name:               pod.Name,
		UID:                pod.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}

	nfgs := make([]nfdv1alpha1.NodeFeatureGroup, 0)
	for _, nodeFeatureGroup := range nodeFeatureGroups {
		// Set metadata with OwnerReference and labels for TTL management
		if nodeFeatureGroup.ObjectMeta.Annotations == nil {
			nodeFeatureGroup.ObjectMeta.Annotations = make(map[string]string)
		}
		if nodeFeatureGroup.ObjectMeta.Labels == nil {
			nodeFeatureGroup.ObjectMeta.Labels = make(map[string]string)
		}
		nodeFeatureGroup.ObjectMeta.GenerateName = "image-compat-" + ownerRef.Name + "-"
		nodeFeatureGroup.ObjectMeta.Name = ""
		nodeFeatureGroup.ObjectMeta.Labels["managed-by"] = PluginName
		nodeFeatureGroup.ObjectMeta.Labels["temporary"] = "true"
		nodeFeatureGroup.ObjectMeta.OwnerReferences = []metav1.OwnerReference{ownerRef}

		fmt.Printf("Processing NodeFeatureGroup : Name=%q, GenerateName=%q, Namespace=%q\n",
			nodeFeatureGroup.ObjectMeta.Name, nodeFeatureGroup.ObjectMeta.GenerateName, nodeFeatureGroup.ObjectMeta.Namespace)
		// Create NodeFeatureGroup CRs in nfd-master namespace
		if nfg, err := cli.NfdV1alpha1().NodeFeatureGroups(namespace).Create(ctx, &nodeFeatureGroup, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("failed to create NodeFeatureGroup: %v", err)
		} else {
			nfgs = append(nfgs, *nfg)
		}
	}
	return nfgs, nil
}

// Transfer the compatibility artifact to node-feature-group
func (fgm *FeatureGroupManagement) TransferFromArtifact(ctx context.Context) ([]nfdv1alpha1.NodeFeatureGroup, error) {
	var nodeFeatureGroups []nfdv1alpha1.NodeFeatureGroup
	spec, err := fgm.artifactClient.FetchCompatibilitySpec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch compatibility spec: %v", err)
	}
	for _, comp := range spec.Compatibilties {
		nodeFeatureGroup := nfdv1alpha1.NodeFeatureGroup{
			Spec: nfdv1alpha1.NodeFeatureGroupSpec{
				Rules: comp.Rules,
			},
		}
		nodeFeatureGroups = append(nodeFeatureGroups, nodeFeatureGroup)
	}
	return nodeFeatureGroups, nil
}
