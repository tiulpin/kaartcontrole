package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

func validateChartValues(defaultValues, providedValues, overridesValues map[string]interface{}, prefix string, issuesFound *bool, ignoreList IgnoreList) {
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
			//fmt.Printf("❌ Unexpected key: '%s' is not defined in chart defaults\n", fullKey)
			//*issuesFound = true
			continue
		}

		if defaultMap, isDefaultMap := defaultValue.(map[string]interface{}); isDefaultMap {
			if providedMap, isProvidedMap := providedValue.(map[string]interface{}); isProvidedMap {
				overridesMap := map[string]interface{}{}
				if overridesValues != nil {
					if overridesSubMap, ok := overridesValues[key].(map[string]interface{}); ok {
						overridesMap = overridesSubMap
					}
				}
				validateChartValues(defaultMap, providedMap, overridesMap, fullKey, issuesFound, ignoreList)
			} else {
				fmt.Printf("❌ Type mismatch for '%s': expected map, got %T\n", fullKey, providedValue)
				*issuesFound = true
			}
			continue
		}

		if reflect.DeepEqual(defaultValue, providedValue) {
			if overridesValues != nil {
				overrideValue, overrideExists := overridesValues[key]
				if overrideExists && !reflect.DeepEqual(overrideValue, providedValue) {
					continue
				}
			}
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

// valuePair represents a candidate pair of values files:
// one overrides file and one service file (e.g. web_service.yaml).
type valuePair struct {
	override string
	service  string
}

// detectPairs searches starting at baseDir (for example, the current working directory)
// for every file named "<chartName>.yaml". For each such service file, it traverses upward
// (but not past baseDir) to locate the nearest overrides.yaml. If found, the pair is recorded.
func detectPairs(baseDir, chartName string) ([]valuePair, error) {
	var pairs []valuePair
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Look for files named "<chartName>.yaml" (e.g. "web_service.yaml")
		if !info.IsDir() && filepath.Base(path) == chartName+".yaml" {
			currentDir := filepath.Dir(path)
			var overridePath string
			// Traverse upward until reaching the baseDir.
			for {
				candidate := filepath.Join(currentDir, "overrides.yaml")
				if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
					overridePath = candidate
					break
				}
				if currentDir == baseDir {
					break
				}
				parent := filepath.Dir(currentDir)
				if parent == currentDir {
					break
				}
				currentDir = parent
			}
			if overridePath != "" {
				pairs = append(pairs, valuePair{override: overridePath, service: path})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort pairs for consistent output.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].override == pairs[j].override {
			return pairs[i].service < pairs[j].service
		}
		return pairs[i].override < pairs[j].override
	})
	return pairs, nil
}

func main() {
	var ignoreList IgnoreList
	var valuesFiles ValueFiles

	flag.Var(&ignoreList, "ignore", "Fields to ignore in validation (can be specified multiple times)")
	flag.Var(&valuesFiles, "f", "Values file (can be specified multiple times)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Printf("Usage: helm kc [--ignore field1,field2,...] <chart> [-f <values-file> ...]\n")
		fmt.Printf("\nExamples:\n")
		fmt.Printf("  helm kc ./mychart -f values.yaml\n")
		fmt.Printf("  helm kc ./mychart -f overrides.yaml -f infra/web_service.yaml\n")
		fmt.Printf("If no -f is provided, the plugin auto-detects valid pairs from the environment tree.\n")
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

	defaultValues := chart.Values

	// If the user provided explicit -f values, merge and validate them as before.
	if len(valuesFiles) > 0 {
		// First, get the values from all but the last file
		var overrideValues map[string]interface{}
		if len(valuesFiles) > 1 {
			overrideOpts := &values.Options{
				ValueFiles: valuesFiles[:len(valuesFiles)-1],
			}
			overrideValues, err = overrideOpts.MergeValues(nil)
			if err != nil {
				fmt.Printf("Failed to load override values: %v\n", err)
				os.Exit(1)
			}
		}

		// Then get all merged values
		valueOpts := &values.Options{
			ValueFiles: valuesFiles,
		}
		providedValues, err := valueOpts.MergeValues(nil)
		if err != nil {
			fmt.Printf("Failed to load values: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\nValidating Helm chart values:\n")
		fmt.Printf("==============================\n")
		fmt.Printf("Chart: %s\n", chartDir)
		fmt.Printf("Values files: %s\n", valuesFiles.String())
		if len(ignoreList) > 0 {
			fmt.Printf("Ignoring fields: %s\n", ignoreList.String())
		}
		fmt.Printf("\nStarting validation...\n\n")

		issuesFound := false
		validateChartValues(defaultValues, providedValues, overrideValues, "", &issuesFound, ignoreList)
		if !issuesFound {
			fmt.Printf("\nValidation completed: No issues found.\n")
		} else {
			fmt.Printf("\nValidation completed: Issues were found.\n")
			os.Exit(1)
		}
		return
	}

	// No -f flags provided: auto-detect valid pairs.
	// Use the current working directory as the base for environment search.
	envDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error determining current directory: %v\n", err)
		os.Exit(1)
	}

	chartName := filepath.Base(chartDir)
	pairs, err := detectPairs(envDir, chartName)
	if err != nil {
		fmt.Printf("Error auto-detecting values: %v\n", err)
		os.Exit(1)
	}
	if len(pairs) == 0 {
		fmt.Printf("No valid values files (overrides.yaml + %s.yaml) found in base directory: %s\n", chartName, envDir)
		os.Exit(1)
	}

	overallIssues := false
	for _, p := range pairs {
		overridesOpts := &values.Options{
			ValueFiles: []string{p.override},
		}
		overridesValues, err := overridesOpts.MergeValues(nil)
		if err != nil {
			fmt.Printf("Failed to load overrides (%s): %v\n", p.override, err)

			continue
		}

		// Then load all merged values
		valueOpts := &values.Options{
			ValueFiles: []string{p.override, p.service},
		}
		providedValues, err := valueOpts.MergeValues(nil)
		if err != nil {
			fmt.Printf("Failed to load values (%s, %s): %v\n", p.override, p.service, err)
			continue
		}

		issuesFound := false
		validateChartValues(defaultValues, providedValues, overridesValues, "", &issuesFound, ignoreList)
		if issuesFound {
			fmt.Printf("Issues found for (%s, %s)\n", p.override, p.service)
			overallIssues = true
		}
	}

	if overallIssues {
		os.Exit(1)
	} else {
		fmt.Printf("\nValidation completed: No issues found.\n")
	}
}
