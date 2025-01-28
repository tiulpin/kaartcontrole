package main

import (
	"testing"
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
