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

// validateDictEnvMapValue catches a common YAML mistake: name/key indented at the same
// level as secretKeyRef (or configMapKeyRef), so they become siblings of the ref instead
// of nested inside it — secretKeyRef ends up null while name/key sit at the top level.
func validateDictEnvMapValue(fullKey string, m map[string]interface{}, issuesFound *bool) {
	validateRefIndent(fullKey, m, "secretKeyRef", issuesFound)
	validateRefIndent(fullKey, m, "configMapKeyRef", issuesFound)
}

func validateRefIndent(fullKey string, m map[string]interface{}, ref string, issuesFound *bool) {
	v, has := m[ref]
	if !has {
		return
	}
	empty := v == nil
	if !empty {
		inner, ok := v.(map[string]interface{})
		if !ok {
			return
		}
		empty = len(inner) == 0
	}
	if !empty {
		return
	}
	for _, sib := range []string{"name", "key"} {
		val, ok := m[sib]
		if !ok || val == nil {
			continue
		}
		if s, isStr := val.(string); isStr && s == "" {
			continue
		}
		fmt.Printf("❌ Invalid '%s': %q must be nested under %s (check YAML indentation)\n", fullKey, sib, ref)
		*issuesFound = true
		return
	}
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
			if prefix == "dictEnv" {
				if em, ok := providedValue.(map[string]interface{}); ok {
					validateDictEnvMapValue(fullKey, em, issuesFound)
				}
			}
			//fmt.Printf("❌ Unexpected key: '%s' is not defined in chart defaults\n", fullKey)
			//*issuesFound = true
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

// valueSet represents all values files that should be merged for a service deployment.
// Files are merged in order, with later files overriding earlier ones.
type valueSet struct {
	allFiles []string // all files in merge order
}

// detectValueSets searches for a generic hierarchical values structure.
// For each service file (chartName.yaml), it looks for:
// 1. Companion app-level files (*.chartName.yaml) in sibling directories
// 2. overrides.yaml in parent directories
// 3. The service file itself
func detectValueSets(baseDir, chartName string) ([]valueSet, error) {
	var sets []valueSet

	// Find all service files and overrides files
	serviceFiles := []string{}
	overridesByDir := make(map[string]string)
	appLevelFiles := make(map[string]string) // serviceName -> path

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		baseName := filepath.Base(path)

		// Collect overrides.yaml files
		if baseName == "overrides.yaml" {
			dir := filepath.Dir(path)
			overridesByDir[dir] = path
		}

		// Collect *.chartName.yaml files (potential app-level configs)
		if strings.HasSuffix(baseName, "."+chartName+".yaml") && baseName != chartName+".yaml" {
			serviceName := strings.TrimSuffix(baseName, "."+chartName+".yaml")
			appLevelFiles[serviceName] = path
		}

		if baseName == chartName+".yaml" {
			serviceFiles = append(serviceFiles, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, servicePath := range serviceFiles {
		relPath, _ := filepath.Rel(baseDir, servicePath)

		var files []string

		// Step 1: Find matching app-level file (if any)
		// Try to infer service name from path structure
		// e.g., services/app/api/web_service.yaml -> try "app-api"
		// or llm/agg/all/web_service.yaml -> try various combinations
		pathParts := strings.Split(filepath.Dir(relPath), string(filepath.Separator))

		// Try different service name patterns
		possibleNames := []string{}
		if len(pathParts) >= 2 {
			// Try: first-last (e.g., app-api from app/api)
			possibleNames = append(possibleNames, pathParts[len(pathParts)-2]+"-"+pathParts[len(pathParts)-1])
		}
		if len(pathParts) >= 3 {
			// Try: first-last for deeper paths
			possibleNames = append(possibleNames, pathParts[len(pathParts)-3]+"-"+pathParts[len(pathParts)-1])
		}

		for _, name := range possibleNames {
			if appPath, exists := appLevelFiles[name]; exists {
				files = append(files, appPath)
				break
			}
		}

		// Step 2: Find overrides.yaml by traversing upward from service file
		currentDir := filepath.Dir(servicePath)
		for {
			if overridePath, exists := overridesByDir[currentDir]; exists {
				files = append(files, overridePath)
				break
			}
			parent := filepath.Dir(currentDir)
			if parent == currentDir || parent == baseDir || !strings.HasPrefix(currentDir, baseDir) {
				break
			}
			currentDir = parent
		}

		// Step 3: Add the service file itself
		files = append(files, servicePath)

		// Only create a set if we have at least overrides + service (minimum 2 files)
		if len(files) >= 2 {
			sets = append(sets, valueSet{allFiles: files})
		}
	}

	sort.Slice(sets, func(i, j int) bool {
		return sets[i].allFiles[len(sets[i].allFiles)-1] < sets[j].allFiles[len(sets[j].allFiles)-1]
	})

	return sets, nil
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
		validateChartValues(defaultValues, providedValues, "", &issuesFound, ignoreList)
		if !issuesFound {
			fmt.Printf("\nValidation completed: No issues found.\n")
		} else {
			fmt.Printf("\nValidation completed: Issues were found.\n")
			os.Exit(1)
		}
		return
	}

	// No -f flags provided: auto-detect value sets.
	// Use the current working directory as the base for environment search.
	envDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error determining current directory: %v\n", err)
		os.Exit(1)
	}

	chartName := filepath.Base(chartDir)
	sets, err := detectValueSets(envDir, chartName)
	if err != nil {
		fmt.Printf("Error auto-detecting values: %v\n", err)
		os.Exit(1)
	}
	if len(sets) == 0 {
		fmt.Printf("No valid value sets found for chart '%s' in directory: %s\n", chartName, envDir)
		fmt.Printf("\nSearching for:\n")
		fmt.Printf("  - Service files: %s.yaml\n", chartName)
		fmt.Printf("  - Override files: overrides.yaml in parent directories\n")
		fmt.Printf("  - App-level files: *.%s.yaml (optional, matched by service name)\n", chartName)
		fmt.Printf("\nNote: Each service file must have at least one overrides.yaml in a parent directory.\n")
		os.Exit(1)
	}

	fmt.Printf("\nValidating Helm chart values:\n")
	fmt.Printf("==============================\n")
	fmt.Printf("Chart: %s\n", chartDir)
	fmt.Printf("Found %d value set(s) to validate\n", len(sets))
	if len(ignoreList) > 0 {
		fmt.Printf("Ignoring fields: %s\n", ignoreList.String())
	}
	fmt.Printf("\n")

	overallIssues := false
	for i, set := range sets {
		serviceFile := set.allFiles[len(set.allFiles)-1]
		rel, _ := filepath.Rel(envDir, serviceFile)

		fmt.Printf("[%d/%d] Validating: %s\n", i+1, len(sets), rel)
		if len(set.allFiles) > 1 {
			fmt.Printf("        Merging values from:\n")
			for _, f := range set.allFiles {
				relFile, _ := filepath.Rel(envDir, f)
				fmt.Printf("          - %s\n", relFile)
			}
		}

		valueOpts := &values.Options{
			ValueFiles: set.allFiles,
		}
		providedValues, err := valueOpts.MergeValues(nil)
		if err != nil {
			fmt.Printf("Failed to merge values: %v\n", err)
			overallIssues = true
			continue
		}

		issuesFound := false
		validateChartValues(defaultValues, providedValues, "", &issuesFound, ignoreList)
		if issuesFound {
			overallIssues = true
		}
		fmt.Printf("\n")
	}

	if overallIssues {
		fmt.Printf("\nValidation completed: Issues were found.\n")
		os.Exit(1)
	} else {
		fmt.Printf("\nValidation completed: No issues found.\n")
	}
}
