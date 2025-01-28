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

```bash
# Basic usage
helm kc ./mychart values.yaml
```

would give

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
helm kc --ignore resources --ignore health ./mychart values.yaml
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
