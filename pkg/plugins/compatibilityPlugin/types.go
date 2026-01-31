package compatibilityPlugin

import (
	"sync"

	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	nfdclientset "sigs.k8s.io/node-feature-discovery/api/generated/clientset/versioned"
	nfdv1alpha1 "sigs.k8s.io/node-feature-discovery/api/nfd/v1alpha1"
)

const (
	// PluginName is the name of this scheduler plugin.
	PluginName = "ImageCompatibilityFilter"
	// NfdMasterLabelSelector is the label selector to find nfd-master pods.
	NfdMasterLabelSelector = "app.kubernetes.io/name=node-feature-discovery,role=master"
)

// ImageCompatibilityPlugin is the main image compatibility filter plugin.
type ImageCompatibilityPlugin struct {
	handle               framework.Handle
	nfdClient            nfdclientset.Interface
	nfdMasterNamespace   string
	args                 ImageCompatibilityPluginArgs
	imageToNFGCache      map[string][]string // Cache: image -> list of NFG names
	imageToNFGCacheMutex sync.RWMutex        // Mutex to protect cache access
}

// ImageCompatibilityPluginArgs holds the arguments for the ImageCompatibilityPlugin.
type ImageCompatibilityPluginArgs struct {
	PlainHttp bool `json:"plainHttp,omitempty"`
}

type Compatibility struct {
	// Rules represents a list of Node Feature Rules.
	Rules []nfdv1alpha1.GroupRule `json:"rules"`
	// Weight indicates the priority of the compatibility set.
	Weight int `json:"weight,omitempty"`
	// Tag enables grouping or distinguishing between compatibility sets.
	Tag string `json:"tag,omitempty"`
	// Description of the compatibility set.
	Description string `json:"description,omitempty"`
}

// CompatibilityState keeps the set of nodes that are compatible with
// the images of a Pod within a single scheduling cycle.
type CompatibilityState struct {
	CompatibleNodes map[string]struct{}
	CreatedNFGs     []string // Names of created NodeFeatureGroup CRs
	Namespace       string   // Namespace where NFGs were created
}

// Clone implements the scheduler framework StateData interface.
func (s *CompatibilityState) Clone() fwk.StateData {
	if s == nil {
		return &CompatibilityState{
			CompatibleNodes: map[string]struct{}{},
			CreatedNFGs:     []string{},
		}
	}
	newMap := make(map[string]struct{}, len(s.CompatibleNodes))
	for k, v := range s.CompatibleNodes {
		newMap[k] = v
	}

	// Deep copy CreatedNFGs slice
	newCreatedNFGs := make([]string, len(s.CreatedNFGs))
	copy(newCreatedNFGs, s.CreatedNFGs)

	return &CompatibilityState{
		CompatibleNodes: newMap,
		CreatedNFGs:     newCreatedNFGs,
		Namespace:       s.Namespace,
	}
}

var (
	_ framework.FilterPlugin    = &ImageCompatibilityPlugin{}
	_ framework.PreFilterPlugin = &ImageCompatibilityPlugin{}
)
