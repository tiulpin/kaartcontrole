package main

import (
	"os"
	"path/filepath"
	"sort"
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
			validateChartValues(tt.defaultValues, tt.providedValues, nil, "", &issuesFound, tt.ignoreList)
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

// TestDetectPairs verifies that detectPairs correctly finds valid pairs of values files.
// For each service file (named "<chartName>.yaml"), detectPairs should locate the nearest
// overrides.yaml (traversing upward until the base directory is reached).
func TestDetectPairs(t *testing.T) {
	// Create a temporary base directory to simulate the environment tree.
	baseDir, err := os.MkdirTemp("", "detectpairs")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(baseDir)

	chartName := "web_service"

	// --- Pair 1 ---
	// Create a directory "pair1" with an overrides.yaml and a service file.
	pair1Dir := filepath.Join(baseDir, "pair1")
	if err := os.MkdirAll(pair1Dir, 0755); err != nil {
		t.Fatalf("failed to create pair1 dir: %v", err)
	}
	override1 := filepath.Join(pair1Dir, "overrides.yaml")
	if err := os.WriteFile(override1, []byte("key: override1"), 0644); err != nil {
		t.Fatalf("failed to write override1: %v", err)
	}
	pair1ServiceDir := filepath.Join(pair1Dir, "services")
	if err := os.MkdirAll(pair1ServiceDir, 0755); err != nil {
		t.Fatalf("failed to create pair1 service dir: %v", err)
	}
	service1 := filepath.Join(pair1ServiceDir, "web_service.yaml")
	if err := os.WriteFile(service1, []byte("key: service1"), 0644); err != nil {
		t.Fatalf("failed to write service1: %v", err)
	}

	// --- Pair 2 ---
	// Create a directory "pair2/sub" with an overrides.yaml and a service file.
	pair2Dir := filepath.Join(baseDir, "pair2", "sub")
	if err := os.MkdirAll(pair2Dir, 0755); err != nil {
		t.Fatalf("failed to create pair2 dir: %v", err)
	}
	override2 := filepath.Join(pair2Dir, "overrides.yaml")
	if err := os.WriteFile(override2, []byte("key: override2"), 0644); err != nil {
		t.Fatalf("failed to write override2: %v", err)
	}
	pair2ServiceDir := filepath.Join(pair2Dir, "services")
	if err := os.MkdirAll(pair2ServiceDir, 0755); err != nil {
		t.Fatalf("failed to create pair2 service dir: %v", err)
	}
	service2 := filepath.Join(pair2ServiceDir, "web_service.yaml")
	if err := os.WriteFile(service2, []byte("key: service2"), 0644); err != nil {
		t.Fatalf("failed to write service2: %v", err)
	}

	// --- No Pair ---
	// Create a directory "nopair" with a service file but no overrides.yaml in its ancestry.
	noPairDir := filepath.Join(baseDir, "nopair", "services")
	if err := os.MkdirAll(noPairDir, 0755); err != nil {
		t.Fatalf("failed to create nopair dir: %v", err)
	}
	noPairService := filepath.Join(noPairDir, "web_service.yaml")
	if err := os.WriteFile(noPairService, []byte("key: nopair"), 0644); err != nil {
		t.Fatalf("failed to write noPairService: %v", err)
	}

	// Also create a file with a different name that should be ignored.
	ignoreFile := filepath.Join(pair1ServiceDir, "not_web_service.yaml")
	if err := os.WriteFile(ignoreFile, []byte("key: ignore"), 0644); err != nil {
		t.Fatalf("failed to write ignoreFile: %v", err)
	}

	// Call detectPairs using the temporary baseDir and the chart name.
	pairs, err := detectPairs(baseDir, chartName)
	if err != nil {
		t.Fatalf("detectPairs returned error: %v", err)
	}

	// We expect exactly 2 pairs (from pair1 and pair2).
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}

	// Sort the pairs by service path for predictable order.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].service < pairs[j].service
	})

	expectedPairs := []struct {
		override string
		service  string
	}{
		{override: override1, service: service1},
		{override: override2, service: service2},
	}

	for i, ep := range expectedPairs {
		if pairs[i].override != ep.override {
			t.Errorf("pair %d: expected override %q, got %q", i, ep.override, pairs[i].override)
		}
		if pairs[i].service != ep.service {
			t.Errorf("pair %d: expected service %q, got %q", i, ep.service, pairs[i].service)
		}
	}
}

func TestValidateChartValuesWithOverrides(t *testing.T) {
	tests := []struct {
		name           string
		defaultValues  map[string]interface{}
		overrideValues map[string]interface{}
		providedValues map[string]interface{}
		ignoreList     IgnoreList
		wantIssues     bool
	}{
		{
			name: "value overridden in overrides.yaml restored to default",
			defaultValues: map[string]interface{}{
				"tempo": map[string]interface{}{
					"enabled": true,
				},
			},
			overrideValues: map[string]interface{}{
				"tempo": map[string]interface{}{
					"enabled": false,
				},
			},
			providedValues: map[string]interface{}{
				"tempo": map[string]interface{}{
					"enabled": true,
				},
			},
			ignoreList: IgnoreList{},
			wantIssues: false, // This should NOT be an issue
		},
		{
			name: "redundant nested value",
			defaultValues: map[string]interface{}{
				"nested": map[string]interface{}{
					"key": "value",
				},
			},
			overrideValues: map[string]interface{}{
				"nested": map[string]interface{}{
					"other": "something",
				},
			},
			providedValues: map[string]interface{}{
				"nested": map[string]interface{}{
					"key": "value", // This matches default and wasn't changed in overrides
				},
			},
			ignoreList: IgnoreList{},
			wantIssues: true, // This should be flagged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var issuesFound bool
			validateChartValues(tt.defaultValues, tt.providedValues, tt.overrideValues, "", &issuesFound, tt.ignoreList)
			if issuesFound != tt.wantIssues {
				t.Errorf("validateChartValues() issuesFound = %v, want %v", issuesFound, tt.wantIssues)
			}
		})
	}
}
