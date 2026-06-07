package tui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestRunDiagnostics — verifies RunDiagnostics output structure
// ---------------------------------------------------------------------------

func TestRunDiagnostics(t *testing.T) {
	result := RunDiagnostics()

	if result == "" {
		t.Fatal("RunDiagnostics should return non-empty output")
	}

	wantSections := []string{
		"Diagnostic Report",
		"Core Configuration",
		"Network Connectivity",
		"Git Version Control",
		"Developer Toolchains",
		"Host System Metrics",
		"Diagnostics complete",
	}

	for _, section := range wantSections {
		if !strings.Contains(result, section) {
			t.Errorf("RunDiagnostics output missing section %q", section)
		}
	}
}
