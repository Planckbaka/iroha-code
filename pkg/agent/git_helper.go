package agent

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitHasChanges checks if there are any staged or unstaged changes in the workspace repository.
func GitHasChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// If not a git repository or git command missing, return false silently
		return false, err
	}
	return strings.TrimSpace(out.String()) != "", nil
}

// GitGetStagedDiff returns the unified diff of changes in the repository.
// It prioritizes staged changes, falling back to unstaged changes to capture recent edits.
func GitGetStagedDiff() (string, error) {
	// 1. Try Cached / Staged diff first
	cmd := exec.Command("git", "diff", "--cached")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	diff := out.String()
	if strings.TrimSpace(diff) != "" {
		return diff, nil
	}

	// 2. Fallback to general unstaged changes
	cmd2 := exec.Command("git", "diff")
	var out2 bytes.Buffer
	cmd2.Stdout = &out2
	if err := cmd2.Run(); err != nil {
		return "", err
	}
	return out2.String(), nil
}

// GitCommit stages all current modifications and commits them with the given message.
func GitCommit(msg string) error {
	// Aider-style automated staging: ensure all untracked/modified files are staged
	addCmd := exec.Command("git", "add", "-A")
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("failed to git add -A: %w", err)
	}

	commitCmd := exec.Command("git", "commit", "-m", msg)
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("failed to git commit: %w", err)
	}
	return nil
}

// GitStageAndDiffPaths stages only the files edited by the current agent turn
// and returns their staged diff. It never stages unrelated workspace changes.
func GitStageAndDiffPaths(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	addArgs := append([]string{"add", "--"}, paths...)
	if err := exec.Command("git", addArgs...).Run(); err != nil {
		return "", fmt.Errorf("failed to stage agent-edited paths: %w", err)
	}

	diffArgs := append([]string{"diff", "--cached", "--"}, paths...)
	var out bytes.Buffer
	cmd := exec.Command("git", diffArgs...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to diff agent-edited paths: %w", err)
	}
	return out.String(), nil
}

// GitCommitPaths commits only the provided paths, leaving unrelated staged and
// unstaged user changes untouched.
func GitCommitPaths(msg string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"commit", "--only", "-m", msg, "--"}, paths...)
	if err := exec.Command("git", args...).Run(); err != nil {
		return fmt.Errorf("failed to commit agent-edited paths: %w", err)
	}
	return nil
}

// GitDirtyPathSet returns absolute paths that already contain user changes.
func GitDirtyPathSet() map[string]bool {
	dirty := make(map[string]bool)
	for _, args := range [][]string{
		{"diff", "--name-only"},
		{"diff", "--cached", "--name-only"},
		{"ls-files", "--others", "--exclude-standard"},
	} {
		output, err := exec.Command("git", args...).Output()
		if err != nil {
			continue
		}
		for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if path == "" {
				continue
			}
			abs, err := filepath.Abs(path)
			if err == nil {
				dirty[abs] = true
			}
		}
	}
	return dirty
}

// FilterInitiallyDirtyPaths excludes files that already had user changes when
// the turn began. Auto-commit must never absorb those changes.
func FilterInitiallyDirtyPaths(paths []string, initiallyDirty map[string]bool) []string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil || initiallyDirty[abs] {
			continue
		}
		filtered = append(filtered, abs)
	}
	return filtered
}
