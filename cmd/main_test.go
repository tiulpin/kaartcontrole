package main

import (
	"os"
	"path/filepath"
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
	defer func() {
		if err := os.Remove(file1.Name()); err != nil {
			t.Logf("failed to remove temp file: %v", err)
		}
	}()

	// Create the second temporary values file.
	file2, err := os.CreateTemp("", "values2-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		if err := os.Remove(file2.Name()); err != nil {
			t.Logf("failed to remove temp file: %v", err)
		}
	}()

	// Write YAML content into the first file.
	content1 := `
key1: value1
nested:
  key2: value2
`
	if _, err := file1.WriteString(content1); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := file1.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

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
	if err := file2.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

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

// TestDetectValueSets verifies that detectValueSets correctly finds valid value sets.
// For each service file (named "<chartName>.yaml"), detectValueSets should locate the nearest
// overrides.yaml (traversing upward until the base directory is reached) and optionally
// matching app-level files.
func TestDetectValueSets(t *testing.T) {
	// Create a temporary base directory to simulate the environment tree.
	baseDir, err := os.MkdirTemp("", "detectvaluesets")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(baseDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	chartName := "web_service"

	// --- Set 1 with app-level file ---
	// Create app-level file
	appDir := filepath.Join(baseDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}
	appLevel1 := filepath.Join(appDir, "my-service.web_service.yaml")
	if err := os.WriteFile(appLevel1, []byte("key: applevel"), 0644); err != nil {
		t.Fatalf("failed to write app level file: %v", err)
	}

	// Create a directory "region1" with an overrides.yaml and a service file.
	region1Dir := filepath.Join(baseDir, "region1")
	if err := os.MkdirAll(region1Dir, 0755); err != nil {
		t.Fatalf("failed to create region1 dir: %v", err)
	}
	override1 := filepath.Join(region1Dir, "overrides.yaml")
	if err := os.WriteFile(override1, []byte("key: override1"), 0644); err != nil {
		t.Fatalf("failed to write override1: %v", err)
	}
	region1ServiceDir := filepath.Join(region1Dir, "services", "my", "service")
	if err := os.MkdirAll(region1ServiceDir, 0755); err != nil {
		t.Fatalf("failed to create region1 service dir: %v", err)
	}
	service1 := filepath.Join(region1ServiceDir, "web_service.yaml")
	if err := os.WriteFile(service1, []byte("key: service1"), 0644); err != nil {
		t.Fatalf("failed to write service1: %v", err)
	}

	// --- Set 2 without app-level file ---
	// Create a directory "region2/sub" with an overrides.yaml and a service file.
	region2Dir := filepath.Join(baseDir, "region2", "sub")
	if err := os.MkdirAll(region2Dir, 0755); err != nil {
		t.Fatalf("failed to create region2 dir: %v", err)
	}
	override2 := filepath.Join(region2Dir, "overrides.yaml")
	if err := os.WriteFile(override2, []byte("key: override2"), 0644); err != nil {
		t.Fatalf("failed to write override2: %v", err)
	}
	region2ServiceDir := filepath.Join(region2Dir, "services")
	if err := os.MkdirAll(region2ServiceDir, 0755); err != nil {
		t.Fatalf("failed to create region2 service dir: %v", err)
	}
	service2 := filepath.Join(region2ServiceDir, "web_service.yaml")
	if err := os.WriteFile(service2, []byte("key: service2"), 0644); err != nil {
		t.Fatalf("failed to write service2: %v", err)
	}

	// --- No Set ---
	// Create a directory "noset" with a service file but no overrides.yaml in its ancestry.
	noSetDir := filepath.Join(baseDir, "noset", "services")
	if err := os.MkdirAll(noSetDir, 0755); err != nil {
		t.Fatalf("failed to create noset dir: %v", err)
	}
	noSetService := filepath.Join(noSetDir, "web_service.yaml")
	if err := os.WriteFile(noSetService, []byte("key: noset"), 0644); err != nil {
		t.Fatalf("failed to write noSetService: %v", err)
	}

	// Also create a file with a different name that should be ignored.
	ignoreFile := filepath.Join(region1ServiceDir, "not_web_service.yaml")
	if err := os.WriteFile(ignoreFile, []byte("key: ignore"), 0644); err != nil {
		t.Fatalf("failed to write ignoreFile: %v", err)
	}

	// Call detectValueSets using the temporary baseDir and the chart name.
	sets, err := detectValueSets(baseDir, chartName)
	if err != nil {
		t.Fatalf("detectValueSets returned error: %v", err)
	}

	// We expect exactly 2 sets (from region1 and region2).
	if len(sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(sets))
	}

	// Verify that set 1 has 3 files (app-level + override + service)
	if len(sets[0].allFiles) != 3 {
		t.Errorf("set 0: expected 3 files, got %d", len(sets[0].allFiles))
	} else {
		// Check that app-level file is included
		if filepath.Base(sets[0].allFiles[0]) != "my-service.web_service.yaml" {
			t.Errorf("set 0: expected first file to be app-level, got %s", sets[0].allFiles[0])
		}
		if filepath.Base(sets[0].allFiles[1]) != "overrides.yaml" {
			t.Errorf("set 0: expected second file to be overrides.yaml, got %s", sets[0].allFiles[1])
		}
		if filepath.Base(sets[0].allFiles[2]) != "web_service.yaml" {
			t.Errorf("set 0: expected third file to be web_service.yaml, got %s", sets[0].allFiles[2])
		}
	}

	// Verify that set 2 has 2 files (override + service, no app-level)
	if len(sets[1].allFiles) != 2 {
		t.Errorf("set 1: expected 2 files, got %d", len(sets[1].allFiles))
	}
}
