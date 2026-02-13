package compatibilityPlugin

import (
	"context"
	"errors"
	"os"
	"reflect"
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
	// Read the actual artifact YAML file
	input, err := os.ReadFile("../../../scripts/compatibility-artifact-kernel-pci.yaml")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Unmarshal into Spec structure
	var mockSpec compatv1alpha1.Spec
	if err := yaml.Unmarshal(input, &mockSpec); err != nil {
		t.Fatalf("failed to unmarshal mock spec: %v", err)
	}

	// Workaround: if Compatibilties is empty, unmarshal directly into slice and set via reflection
	// This is needed because yaml.Unmarshal may not populate the field due to tag mismatches
	if len(mockSpec.Compatibilties) == 0 {
		var raw map[string]interface{}
		if err := yaml.Unmarshal(input, &raw); err == nil {
			if compat, ok := raw["compatibilities"]; ok {
				compatBytes, _ := yaml.Marshal(compat)
				var compatList []compatv1alpha1.Compatibility
				if err := yaml.Unmarshal(compatBytes, &compatList); err == nil && len(compatList) > 0 {
					// Use reflection to set the field
					specValue := reflect.ValueOf(&mockSpec).Elem()
					field := specValue.FieldByName("Compatibilties")
					if field.IsValid() && field.CanSet() {
						field.Set(reflect.ValueOf(compatList))
					}
				}
			}
		}
	}

	fgm := &FeatureGroupManagement{
		artifactClient: &MockArtifactClient{spec: &mockSpec},
	}

	nodeFeatureGroups, err := fgm.TransferFromArtifact(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(nodeFeatureGroups) != 1 {
		t.Fatalf("expected 1 NodeFeatureGroup, got %d", len(nodeFeatureGroups))
	}
	if len(nodeFeatureGroups[0].Spec.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(nodeFeatureGroups[0].Spec.Rules))
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
