#!/usr/bin/env bash
set -euo pipefail

#
# This script installs the KaartControle plugin to your local Helm plugins directory
# for quick development & testing.
#

# 1) Remove any old version of the plugin
helm plugin remove kc 2>/dev/null || true

# 2) Install the plugin from the current directory
helm plugin install .