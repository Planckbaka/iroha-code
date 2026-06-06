#!/usr/bin/env bash
#
# benchmark_coverage.sh — Measure average test coverage across pkg/agent, pkg/tui, pkg/llm, pkg/config.
#
# Output: a single JSON line to stdout:
#   {"primary": 75.3, "sub_scores": {"pkg/agent": 72.1, ...}}
#
# Exit 0 on success, non-zero on failure.
# Deterministic: same code + same tests = same score every run.
# Self-contained: no external services required.
# Compatible with bash 3.2+ (no associative arrays).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

COVERPROFILE="$(mktemp /tmp/coverage-XXXXXX.out)"
trap 'rm -f "$COVERPROFILE"' EXIT

# Run tests with coverage across all pkg subdirectories.
if ! go test -skip "TestBlockingConfirmationTool_AskFlow|TestShellRunHandler" -coverprofile="$COVERPROFILE" ./pkg/agent/... ./pkg/tui/... ./pkg/llm/... ./pkg/config/... > /dev/null 2>&1; then
    echo "ERROR: go test failed" >&2
    exit 1
fi

# Extract per-package coverage using go tool cover -func.
# Output lines look like:
#   iroha/pkg/agent/runner.go:45:          SomeFunc       72.1%
# We parse the last column (percentage) for each file in each package,
# then compute a per-package average across all functions in that package.
#
# We use awk for all arithmetic to avoid dependency on bc and associative arrays.

go tool cover -func="$COVERPROFILE" | awk '
BEGIN {
    # Initialize package sums and counts
    pkgs[1] = "pkg/agent"
    pkgs[2] = "pkg/config"
    pkgs[3] = "pkg/llm"
    pkgs[4] = "pkg/tui"
    for (i = 1; i <= 4; i++) {
        psum[pkgs[i]] = 0
        pcount[pkgs[i]] = 0
    }
}
{
    # Only process lines that contain a coverage percentage (last field ends with %)
    n = split($0, fields, /\t+/)
    last = $NF
    if (last ~ /%$/) {
        pct = last
        sub(/%$/, "", pct)
        line = $0
        for (i = 1; i <= 4; i++) {
            pkg = pkgs[i]
            # Match lines for this package
            if (index(line, "iroha/" pkg "/") > 0) {
                psum[pkg] += pct + 0
                pcount[pkg]++
                break
            }
        }
    }
}
END {
    total = 0
    for (i = 1; i <= 4; i++) {
        pkg = pkgs[i]
        if (pcount[pkg] > 0) {
            avg = psum[pkg] / pcount[pkg]
        } else {
            avg = 0.0
        }
        pavg[pkg] = sprintf("%.1f", avg)
        total += avg
    }
    primary = sprintf("%.1f", total / 4)

    # Build JSON with deterministic key order (alphabetical)
    printf "{\"primary\": %s, \"sub_scores\": {\"pkg/agent\": %s, \"pkg/config\": %s, \"pkg/llm\": %s, \"pkg/tui\": %s}}\n",
        primary, pavg["pkg/agent"], pavg["pkg/config"], pavg["pkg/llm"], pavg["pkg/tui"]
}
'
