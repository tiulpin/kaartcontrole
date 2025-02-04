package main

import (
	"os"
	"testing"

	"helm.sh/helm/v3/pkg/cli/values"
)

func TestValidateChartValues(t *testing.T) {
	tests := []struct {
		name           string
		defaultValues  map[string]interface{}
		providedValues map[string]interface{}
		ignoreList     IgnoreList
		wantIssues     bool
	}{
		{
			name: "no issues",
			defaultValues: map[string]interface{}{
				"key1": "value1",
			},
			providedValues: map[string]interface{}{
				"key1": "different",
			},
			ignoreList: IgnoreList{},
			wantIssues: false,
		},
		{
			name: "redundant value",
			defaultValues: map[string]interface{}{
				"key1": "value1",
			},
			providedValues: map[string]interface{}{
				"key1": "value1",
			},
			ignoreList: IgnoreList{},
			wantIssues: true,
		},
		{
			name: "type mismatch",
			defaultValues: map[string]interface{}{
				"key1": "value1",
			},
			providedValues: map[string]interface{}{
				"key1": 123,
			},
			ignoreList: IgnoreList{},
			wantIssues: true,
		},
		{
			name: "ignored field",
			defaultValues: map[string]interface{}{
				"resources": map[string]interface{}{
					"limits": map[string]interface{}{
						"cpu": "100m",
					},
				},
			},
			providedValues: map[string]interface{}{
				"resources": map[string]interface{}{
					"limits": map[string]interface{}{
						"cpu": 1,
					},
				},
			},
			ignoreList: IgnoreList{"resources"},
			wantIssues: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var issuesFound bool
			validateChartValues(tt.defaultValues, tt.providedValues, "", &issuesFound, tt.ignoreList)
			if issuesFound != tt.wantIssues {
				t.Errorf("validateChartValues() issuesFound = %v, want %v", issuesFound, tt.wantIssues)
			}
		})
	}
}

// TestMergeValues verifies that merging multiple values files produces the expected result.
// In this test, we create two temporary YAML files. Values from the second file should override
// those from the first.
func TestMergeValues(t *testing.T) {
	// Create the first temporary values file.
	file1, err := os.CreateTemp("", "values1-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file1.Name())

	// Create the second temporary values file.
	file2, err := os.CreateTemp("", "values2-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file2.Name())

	// Write YAML content into the first file.
	content1 := `
key1: value1
nested:
  key2: value2
`
	if _, err := file1.WriteString(content1); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	file1.Close()

	// Write YAML content into the second file.
	content2 := `
key1: override
nested:
  key2: override2
  key3: value3
`
	if _, err := file2.WriteString(content2); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	file2.Close()

	// Use Helm's values.Options to merge the two files.
	valueOpts := &values.Options{
		ValueFiles: []string{file1.Name(), file2.Name()},
	}
	merged, err := valueOpts.MergeValues(nil)
	if err != nil {
		t.Fatalf("MergeValues() returned error: %v", err)
	}

	// Check that the key from the second file overrides the first.
	if merged["key1"] != "override" {
		t.Errorf("expected key1 to be 'override', got %v", merged["key1"])
	}

	// Verify nested keys.
	nested, ok := merged["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested to be map[string]interface{}")
	}
	if nested["key2"] != "override2" {
		t.Errorf("expected nested.key2 to be 'override2', got %v", nested["key2"])
	}
	if nested["key3"] != "value3" {
		t.Errorf("expected nested.key3 to be 'value3', got %v", nested["key3"])
	}
}
