package compatibilityPlugin

import (
	"context"
	"errors"
	"testing"

	"gopkg.in/yaml.v3"
	compatv1alpha1 "sigs.k8s.io/node-feature-discovery/api/image-compatibility/v1alpha1"
)

// MockArtifactClient mocks artifactcli.ArtifactClient
type MockArtifactClient struct {
	spec *compatv1alpha1.Spec
	err  error
}

func (m *MockArtifactClient) FetchCompatibilitySpec(ctx context.Context) (*compatv1alpha1.Spec, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.spec, nil
}

func TestTransferFromArtifact_Success(t *testing.T) {

	var input = `
	version: v1alpha1
	compatibilities:
	- description: "My image requirements"
  	  rules:
  	  - name: "kernel and cpu"
  	    matchFeatures:
  	    - feature: kernel.loadedmodule
  	      matchExpressions:
  	        vfio-pci: {op: Exists}
  	    - feature: cpu.model
  	      matchExpressions:
  	        vendor_id: {op: In, value: ["Intel", "AMD"]}
  	  - name: "one of available nics"
  	    matchAny:
  	    - matchFeatures:
  	      - feature: pci.device
  	        matchExpressions:
  	          vendor: {op: In, value: ["0eee"]}
  	          class: {op: In, value: ["0200"]}
  	    - matchFeatures:
  	      - feature: pci.device
  	        matchExpressions:
  	          vendor: {op: In, value: ["0fff"]}
  	          class: {op: In, value: ["0200"]}
	`
	var mockSpec compatv1alpha1.Spec
	err := yaml.Unmarshal([]byte(input), &mockSpec)
	if err != nil {
		t.Fatalf("failed to unmarshal mock spec: %v", err)
	}
	fgm := &FeatureGroupManagement{
		artifactClient: &MockArtifactClient{spec: &mockSpec},
	}

	// Patch method signature for test compatibility
	orig := fgm.artifactClient
	defer func() { fgm.artifactClient = orig }()

	// Patch FetchCompatibilitySpec to return nfdv1alpha1.CompatibilitySpec
	fgm.artifactClient = &struct {
		*MockArtifactClient
	}{
		MockArtifactClient: &MockArtifactClient{spec: &mockSpec},
	}
	// Use type assertion to simulate the expected return type
	fgm.artifactClient = &MockArtifactClient{
		spec: &mockSpec,
	}

	nodeFeatureGroups, err := fgm.TransferFromArtifact(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(nodeFeatureGroups) != 1 {
		t.Fatalf("expected 1 NodeFeatureGroup, got %d", len(nodeFeatureGroups))
	}
	if len(nodeFeatureGroups[0].Spec.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(nodeFeatureGroups[0].Spec.Rules))
	}
}

func TestTransferFromArtifact_FetchError(t *testing.T) {
	fgm := &FeatureGroupManagement{
		artifactClient: &MockArtifactClient{err: errors.New("fetch error")},
	}
	nodeFeatureGroups, err := fgm.TransferFromArtifact(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if nodeFeatureGroups != nil {
		t.Errorf("expected nil nodeFeatureGroups, got %v", nodeFeatureGroups)
	}
}
