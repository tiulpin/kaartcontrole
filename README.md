# helm kaartcontrole `kc`

_KaartControle_ (💡 a chart check, in Dutch) is a [`helm`](https://helm.sh) plugin to quickly validate chart values against defaults and detect redundant or mismatched values.

### Motivation

- I just wanted to play around with helm plugins
- I haven't found a proper solution for a task I spent some time on
- Now it's focused around basic sanity check to reduce duplicates and unwanted values, could be expanded more in the future

## Installation

```bash
helm plugin install https://github.com/tiulpin/kaartcontrole
```

## Usage

### Basic usage with explicit values files

```bash
# Validate with explicit values files
helm kc ./mychart -f values.yaml
helm kc ./mychart -f overrides.yaml -f service.yaml
```

```text
Validating Helm chart values:
==============================
Chart: ./mychart
Values file: values.yaml

Starting validation...

❌ Unexpected key: 'maxReplicaCount' is not defined in chart defaults
⚠️ Redundant value: 'resources.requests.cpu' matches default value: 100m
❌ Type mismatch for 'resources.limits.cpu': expected string, got float64

Validation completed: Issues were found.
Error: plugin "kc" exited with error
```

... which has some issues! Let's remove `maxReplicaCount` and run the check again ignoring the fields we don't care much:

```bash
# Ignore specific fields
helm kc --ignore resources --ignore health ./mychart -f values.yaml
```

```text
Validating Helm chart values:
==============================
Chart: ./mychart
Values file: values.yaml
Ignoring fields: resources,health

Starting validation...


Validation completed: No issues found.
```

## Options

* `--ignore`: Fields to ignore in validation (can be specified multiple times)
* `-f`: Explicitly specify values files to merge (can be specified multiple times). If not provided, auto-detection is used.
