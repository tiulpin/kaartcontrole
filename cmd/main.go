package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
)

// IgnoreList holds fields to be ignored during validation.
type IgnoreList []string

func (i *IgnoreList) String() string {
	return strings.Join(*i, ",")
}

func (i *IgnoreList) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// ValueFiles holds the list of values files passed via -f.
type ValueFiles []string

func (v *ValueFiles) String() string {
	return strings.Join(*v, ",")
}

func (v *ValueFiles) Set(value string) error {
	*v = append(*v, value)
	return nil
}

func shouldIgnore(path string, ignoreList IgnoreList) bool {
	for _, ignore := range ignoreList {
		if strings.HasPrefix(path, ignore) {
			return true
		}
	}
	return false
}

func validateChartValues(defaultValues, providedValues map[string]interface{}, prefix string, issuesFound *bool, ignoreList IgnoreList) {
	for key, providedValue := range providedValues {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if shouldIgnore(fullKey, ignoreList) {
			continue
		}

		defaultValue, exists := defaultValues[key]
		if !exists {
			fmt.Printf("❌ Unexpected key: '%s' is not defined in chart defaults\n", fullKey)
			*issuesFound = true
			continue
		}

		if defaultMap, isDefaultMap := defaultValue.(map[string]interface{}); isDefaultMap {
			if providedMap, isProvidedMap := providedValue.(map[string]interface{}); isProvidedMap {
				validateChartValues(defaultMap, providedMap, fullKey, issuesFound, ignoreList)
			} else {
				fmt.Printf("❌ Type mismatch for '%s': expected map, got %T\n", fullKey, providedValue)
				*issuesFound = true
			}
			continue
		}

		if reflect.DeepEqual(defaultValue, providedValue) {
			fmt.Printf("⚠️  Redundant value: '%s' matches default value: %v\n", fullKey, providedValue)
			*issuesFound = true
			continue
		}

		if defaultValue != nil && providedValue != nil {
			defaultType := reflect.TypeOf(defaultValue)
			providedType := reflect.TypeOf(providedValue)

			if defaultType != providedType {
				fmt.Printf("❌ Type mismatch for '%s': expected %T, got %T\n", fullKey, defaultValue, providedValue)
				*issuesFound = true
			}
		}
	}
}

func findChart(chartPath string) (string, error) {
	if strings.HasPrefix(chartPath, "/") || strings.HasPrefix(chartPath, "./") || strings.HasPrefix(chartPath, "../") {
		return chartPath, nil
	}

	if _, err := os.Stat(chartPath); err == nil {
		absPath, err := filepath.Abs(chartPath)
		if err != nil {
			return "", err
		}
		return absPath, nil
	}

	helmHome := os.Getenv("HELM_HOME")
	if helmHome == "" {
		helmHome = filepath.Join(os.Getenv("HOME"), ".helm")
	}
	cachePath := filepath.Join(helmHome, "cache", "charts", chartPath)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	return "", fmt.Errorf("chart not found: %s", chartPath)
}

func main() {
	var ignoreList IgnoreList
	var valuesFiles ValueFiles

	flag.Var(&ignoreList, "ignore", "Fields to ignore in validation (can be specified multiple times)")
	flag.Var(&valuesFiles, "f", "Values file (can be specified multiple times)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 || len(valuesFiles) == 0 {
		fmt.Printf("Usage: helm kc [--ignore field1,field2,...] <chart> -f <values-file> [-f <additional-values-file> ...]\n")
		fmt.Printf("\nExamples:\n")
		fmt.Printf("  helm kc ./mychart -f values.yaml\n")
		fmt.Printf("  helm kc ./mychart -f overrides.yaml -f infra/web_service.yaml\n")
		os.Exit(1)
	}

	chartPath := args[0]

	chartDir, err := findChart(chartPath)
	if err != nil {
		fmt.Printf("Error locating chart: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(chartDir); os.IsNotExist(err) {
		fmt.Printf("Chart directory does not exist: %s\n", chartDir)
		os.Exit(1)
	}

	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), nil); err != nil {
		fmt.Printf("Failed to initialize Helm configuration: %v\n", err)
		os.Exit(1)
	}

	chart, err := loader.Load(chartDir)
	if err != nil {
		fmt.Printf("Failed to load chart: %v\n", err)
		os.Exit(1)
	}

	valueOpts := &values.Options{
		ValueFiles: valuesFiles,
	}
	providedValues, err := valueOpts.MergeValues(nil)
	if err != nil {
		fmt.Printf("Failed to load values: %v\n", err)
		os.Exit(1)
	}
	defaultValues := chart.Values

	fmt.Printf("\nValidating Helm chart values:\n")
	fmt.Printf("==============================\n")
	fmt.Printf("Chart: %s\n", chartDir)
	fmt.Printf("Values files: %s\n", valuesFiles.String())
	if len(ignoreList) > 0 {
		fmt.Printf("Ignoring fields: %s\n", ignoreList.String())
	}
	fmt.Printf("\nStarting validation...\n\n")

	issuesFound := false
	validateChartValues(defaultValues, providedValues, "", &issuesFound, ignoreList)

	if !issuesFound {
		fmt.Printf("\nValidation completed: No issues found.\n")
	} else {
		fmt.Printf("\nValidation completed: Issues were found.\n")
		os.Exit(1)
	}
}
