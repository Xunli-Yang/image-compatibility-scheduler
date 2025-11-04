package compatibilityPlugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	nfdvalidator "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/node-validator"
)

/**********************test parseLogs function********************/
func TestParseLogs_ValidJSON(t *testing.T) {
	compat := []nfdvalidator.CompatibilityStatus{
		{
			Rules: []nfdvalidator.ProcessedRuleStatus{
				{IsMatch: true, Name: "rule1"},
				{IsMatch: false, Name: "rule2"},
			},
		},
	}
	jsonBytes, _ := json.Marshal(compat)
	logs := "[{" + string(jsonBytes[2:len(jsonBytes)-2]) + "}]"

	result, err := parseLogs(logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 compatibility result, got %d", len(result))
	}
	if len(result[0].Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(result[0].Rules))
	}
}
func TestParseLogs_JSONExample1(t *testing.T) {
	logs := `[{"rules":[{"name":"kernel and cpu","isMatch":false,"matchedExpressions":[{"feature":"cpu.model","name":"vendor_id","expression":{"op":"In","value":["Intel","AMD"]},"matcherType":"matchExpression","isMatch":false}]},{"name":"one of available nics","isMatch":false,"matchedAny":[{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0eee"]},"matcherType":"matchExpression","isMatch":false}]},{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0fff"]},"matcherType":"matchExpression","isMatch":false}]}]}],"description":"My image requirements"}]`
	result, err := parseLogs(logs)
	fmt.Println("parse result:", result)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 compatibility result, got %d", len(result))
	}
	if len(result[0].Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(result[0].Rules))
	}
}

func TestParseLogs_InvalidJSON(t *testing.T) {
	logs := "some logs without valid json"
	_, err := parseLogs(logs)
	fmt.Println(err)
	if err == nil {
		t.Errorf("expected error for invalid logs, got nil")
	}
}

func TestParseLogs_EmptyJSON(t *testing.T) {
	logs := ""
	_, err := parseLogs(logs)
	if err == nil {
		t.Errorf("expected error for empty logs, got nil")
	}
}

func TestParseLogs_MalformedJSON(t *testing.T) {
	logs := "[{invalid json}]"
	_, err := parseLogs(logs)
	fmt.Println(err)
	if err == nil {
		t.Errorf("expected error for malformed json, got nil")
	}
}

/**********************test parseValidationResult function********************/
func TestParseValidationResult_AllCompatible(t *testing.T) {
	logs := `[{"rules":[{"name":"rule1","isMatch":true},{"name":"rule2","isMatch":true}]}]`
	nodeName := "node1"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Compatible {
		t.Errorf("expected Compatible=true, got false")
	}
	if result.Reason != "" {
		t.Errorf("expected empty Reason, got %s", result.Reason)
	}
}

func TestParseValidationResult_SomeIncompatible(t *testing.T) {
	logs := `[{"rules":[{"name":"rule1","isMatch":true},{"name":"rule2","isMatch":false}]}]`
	nodeName := "node2"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Compatible {
		t.Errorf("expected Compatible=false, got true")
	}
	if result.Reason == "" {
		t.Errorf("expected Reason to be set, got empty string")
	}
	if got := result.Reason; got == "" || !strings.Contains(got, "Incompatible on node node2") {
		t.Errorf("unexpected Reason: %s", got)
	}
}

func TestParseValidationResult_AllIncompatible(t *testing.T) {
	logs := `[{"rules":[{"name":"rule1","isMatch":false},{"name":"rule2","isMatch":false}]}]`
	nodeName := "node3"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Compatible {
		t.Errorf("expected Compatible=false, got true")
	}
	if result.Reason == "" {
		t.Errorf("expected Reason to be set, got empty string")
	}
}

func TestParseValidationResult_InvalidJSON(t *testing.T) {
	logs := `not a json`
	nodeName := "node4"
	_, err := parseValidationResult(nodeName, logs)
	if err == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}

func TestParseValidationResult_EmptyLogs(t *testing.T) {
	logs := ""
	nodeName := "node5"
	_, err := parseValidationResult(nodeName, logs)
	if err == nil {
		t.Errorf("expected error for empty logs, got nil")
	}
}

func TestParseValidationResult_JSONExample1_allfalse(t *testing.T) {
	logs := `[{"rules":[{"name":"kernel and cpu","isMatch":false,"matchedExpressions":[{"feature":"cpu.model","name":"vendor_id","expression":{"op":"In","value":["Intel","AMD"]},"matcherType":"matchExpression","isMatch":false}]},{"name":"one of available nics","isMatch":false,"matchedAny":[{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0eee"]},"matcherType":"matchExpression","isMatch":false}]},{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0fff"]},"matcherType":"matchExpression","isMatch":false}]}]}],"description":"My image requirements"}]`
	nodeName := "node-example"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Compatible {
		t.Errorf("expected Compatible=false, got true")
	}
	if result.Reason == "" {
		t.Errorf("expected Reason to be set, got empty string")
	}
	fmt.Println("Validation Result:", result)
}

func TestParseValidationResult_JSONExample2_alltrue(t *testing.T) {
	logs := `[{"rules":[{"name":"kernel and cpu","isMatch":true,"matchedExpressions":[{"feature":"cpu.model","name":"vendor_id","expression":{"op":"In","value":["Intel","AMD"]},"matcherType":"matchExpression","isMatch":true}]},{"name":"one of available nics","isMatch":true,"matchedAny":[{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0eee"]},"matcherType":"matchExpression","isMatch":true}]},{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0fff"]},"matcherType":"matchExpression","isMatch":true}]}]}],"description":"My image requirements"}]`
	nodeName := "node-example"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Compatible {
		t.Errorf("expected Compatible=true, got false")
	}
	if result.Reason == "" {
		t.Errorf("expected Reason to be set, got empty string")
	}
	fmt.Println("Validation Result:", result)
}

func TestParseValidationResult_JSONExample3_mixed(t *testing.T) {
	logs := `[{"rules":[{"name":"kernel and cpu","isMatch":true,"matchedExpressions":[{"feature":"cpu.model","name":"vendor_id","expression":{"op":"In","value":["Intel","AMD"]},"matcherType":"matchExpression","isMatch":true}]},{"name":"one of available nics","isMatch":false,"matchedAny":[{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0eee"]},"matcherType":"matchExpression","isMatch":false}]},{"matchedExpressions":[{"feature":"pci.device","name":"vendor","expression":{"op":"In","value":["0fff"]},"matcherType":"matchExpression","isMatch":false}]}]}],"description":"My image requirements"}]`
	nodeName := "node-example"
	result, err := parseValidationResult(nodeName, logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Compatible {
		t.Errorf("expected Compatible=false, got true")
	}
	if result.Reason == "" {
		t.Errorf("expected Reason to be set, got empty string")
	}
	fmt.Println("Validation Result:", result)
}
