package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MemoryManager.Update tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Update_Success(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	// Save initial entry
	err := mm.Save("test_entry", "Original description", MemTypeUser, "Original content.")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update the entry
	err = mm.Update("test_entry", "Updated description", MemTypeProject, "Updated content.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify the update
	entries := mm.List()
	projectEntries := entries[MemTypeProject]
	if len(projectEntries) != 1 {
		t.Fatalf("expected 1 project entry, got %d", len(projectEntries))
	}
	e := projectEntries[0]
	if e.Name != "test_entry" {
		t.Errorf("Name = %q, want %q", e.Name, "test_entry")
	}
	if e.Description != "Updated description" {
		t.Errorf("Description = %q, want %q", e.Description, "Updated description")
	}
	if !strings.Contains(e.Content, "Updated content.") {
		t.Errorf("Content = %q, want to contain 'Updated content.'", e.Content)
	}
	if e.Type != MemTypeProject {
		t.Errorf("Type = %q, want %q", e.Type, MemTypeProject)
	}
}

func TestMemoryManager_Update_NotFound(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	err := mm.Update("nonexistent", "desc", MemTypeUser, "content")
	if err == nil {
		t.Error("expected error when updating nonexistent entry")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestMemoryManager_Update_InvalidType(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("valid_entry", "desc", MemTypeUser, "content")

	err := mm.Update("valid_entry", "desc", "invalid_type", "content")
	if err == nil {
		t.Error("expected error for invalid memory type")
	}
	if !strings.Contains(err.Error(), "invalid memory type") {
		t.Errorf("expected 'invalid memory type' error, got: %v", err)
	}
}

func TestMemoryManager_Update_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("file_test", "Original", MemTypeUser, "Old content.")

	err := mm.Update("file_test", "Updated", MemTypeUser, "New content.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify file on disk was updated
	expectedFile := filepath.Join(dir, ".iroha", "memory", "file_test.md")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "New content.") {
		t.Errorf("file should contain updated content, got:\n%s", content)
	}
	if !strings.Contains(content, "Updated") {
		t.Errorf("file should contain updated description, got:\n%s", content)
	}
}

func TestMemoryManager_Update_UpdatedAtTimestamp(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("timestamp_test", "Original", MemTypeUser, "content.")

	before := time.Now().UTC()
	time.Sleep(10 * time.Millisecond) // ensure time passes

	err := mm.Update("timestamp_test", "Updated", MemTypeUser, "new content.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	after := time.Now().UTC()

	entries := mm.List()
	userEntries := entries[MemTypeUser]
	if len(userEntries) != 1 {
		t.Fatalf("expected 1 user entry, got %d", len(userEntries))
	}
	e := userEntries[0]
	if e.UpdatedAt.Before(before) || e.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, expected between %v and %v", e.UpdatedAt, before, after)
	}
}

func TestMemoryManager_Update_RebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("index_test", "Original desc", MemTypeUser, "content.")

	err := mm.Update("index_test", "Updated desc", MemTypeUser, "new content.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify MEMORY.md was updated
	indexFile := filepath.Join(dir, ".iroha", "memory", "MEMORY.md")
	data, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatalf("MEMORY.md not found: %v", err)
	}
	idx := string(data)
	if !strings.Contains(idx, "Updated desc") {
		t.Errorf("MEMORY.md should contain 'Updated desc', got:\n%s", idx)
	}
}

func TestMemoryManager_Update_PreservesEntryCount(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("entry1", "desc1", MemTypeUser, "content1.")
	_ = mm.Save("entry2", "desc2", MemTypeFeedback, "content2.")

	beforeCount := mm.Count()

	err := mm.Update("entry1", "updated1", MemTypeUser, "updated content1.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	afterCount := mm.Count()
	if afterCount != beforeCount {
		t.Errorf("count changed from %d to %d after update", beforeCount, afterCount)
	}
}

func TestMemoryManager_Update_TypeChange(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("type_change", "desc", MemTypeUser, "content.")

	err := mm.Update("type_change", "desc", MemTypeReference, "content.")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	entries := mm.List()
	// Should no longer be under user
	if len(entries[MemTypeUser]) != 0 {
		t.Errorf("expected 0 user entries after type change, got %d", len(entries[MemTypeUser]))
	}
	// Should now be under reference
	if len(entries[MemTypeReference]) != 1 {
		t.Errorf("expected 1 reference entry after type change, got %d", len(entries[MemTypeReference]))
	}
}

func TestMemoryManager_Update_Concurrent(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	// Create multiple entries
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("concurrent_%d", i)
		_ = mm.Save(name, "original", MemTypeUser, "content.")
	}

	// Update all concurrently
	errCh := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func(i int) {
			name := fmt.Sprintf("concurrent_%d", i)
			errCh <- mm.Update(name, "updated", MemTypeUser, "new content.")
		}(i)
	}

	for i := 0; i < 5; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent update %d failed: %v", i, err)
		}
	}

	if mm.Count() != 5 {
		t.Errorf("expected 5 entries after concurrent updates, got %d", mm.Count())
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.Reload tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Reload(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("reload_test", "desc", MemTypeUser, "content.")

	if mm.Count() != 1 {
		t.Fatalf("expected 1 entry before reload, got %d", mm.Count())
	}

	// Reload should re-read from disk
	mm.Reload()

	if mm.Count() != 1 {
		t.Errorf("expected 1 entry after reload, got %d", mm.Count())
	}

	entries := mm.List()
	userEntries := entries[MemTypeUser]
	if len(userEntries) != 1 {
		t.Fatalf("expected 1 user entry after reload, got %d", len(userEntries))
	}
	if userEntries[0].Name != "reload_test" {
		t.Errorf("Name = %q, want %q", userEntries[0].Name, "reload_test")
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.Search tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Search_FindsByName(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("deploy_process", "How to deploy", MemTypeProject, "Use kubectl apply.")
	_ = mm.Save("code_style", "Use tabs", MemTypeUser, "Tab indentation.")

	results := mm.Search("deploy")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "deploy_process" {
		t.Errorf("expected 'deploy_process', got %q", results[0].Name)
	}
}

func TestMemoryManager_Search_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("test", "desc", MemTypeUser, "content.")

	results := mm.Search("")
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestMemoryManager_Search_NoMatch(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("test", "desc", MemTypeUser, "content.")

	results := mm.Search("xyzzy_no_match")
	if len(results) != 0 {
		t.Errorf("expected 0 results for no match, got %d", len(results))
	}
}

func TestMemoryManager_Search_RankedByRelevance(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("deploy_prod", "Deploy to production", MemTypeProject, "production deploy steps")
	_ = mm.Save("deploy_staging", "Deploy staging only", MemTypeProject, "staging")
	_ = mm.Save("coffee_prefs", "Coffee preferences", MemTypeUser, "latte art")

	results := mm.Search("deploy production")
	if len(results) != 2 {
		t.Fatalf("expected 2 results matching 'deploy production', got %d", len(results))
	}
	// deploy_prod should rank higher (matches both "deploy" and "production")
	if results[0].Name != "deploy_prod" {
		t.Errorf("expected highest ranked result 'deploy_prod', got %q", results[0].Name)
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.GetDirs tests
// ---------------------------------------------------------------------------

func TestMemoryManager_GetDirs(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	// Save something so dirs get populated
	_ = mm.Save("dir_test", "desc", MemTypeUser, "content.")

	dirs := mm.GetDirs()
	if len(dirs) == 0 {
		t.Error("expected at least 1 directory after saving")
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.Count tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Count_Empty(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	if mm.Count() != 0 {
		t.Errorf("expected 0 count for empty manager, got %d", mm.Count())
	}
}

// ---------------------------------------------------------------------------
// parseFrontmatter edge cases
// ---------------------------------------------------------------------------

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	_, err := parseFrontmatter("just plain text without frontmatter")
	if err == nil {
		t.Error("expected error for text without frontmatter")
	}
}

func TestParseFrontmatter_MissingName(t *testing.T) {
	text := "---\ndescription: test\ntype: user\n---\ncontent"
	_, err := parseFrontmatter(text)
	if err == nil {
		t.Error("expected error for missing 'name' field")
	}
}

func TestParseFrontmatter_ValidEntry(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339)
	text := fmt.Sprintf("---\nname: test_entry\ndescription: A test\ntype: user\nupdated_at: %s\n---\nHello world", ts)
	entry, err := parseFrontmatter(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Name != "test_entry" {
		t.Errorf("Name = %q, want %q", entry.Name, "test_entry")
	}
	if entry.Description != "A test" {
		t.Errorf("Description = %q, want %q", entry.Description, "A test")
	}
	if entry.Type != MemTypeUser {
		t.Errorf("Type = %q, want %q", entry.Type, MemTypeUser)
	}
	if entry.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", entry.Content, "Hello world")
	}
}

func TestParseFrontmatter_ExtraFields(t *testing.T) {
	text := "---\nname: extra\nfoo: bar\ntype: project\n---\ncontent"
	entry, err := parseFrontmatter(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Name != "extra" {
		t.Errorf("Name = %q, want %q", entry.Name, "extra")
	}
}

// ---------------------------------------------------------------------------
// renderFrontmatter round-trip
// ---------------------------------------------------------------------------

func TestRenderFrontmatter_RoundTrip(t *testing.T) {
	original := &MemoryEntry{
		Name:        "round_trip",
		Description: "Test round trip",
		Type:        MemTypeFeedback,
		Content:     "Some content here",
		UpdatedAt:   time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		File:        "round_trip.md",
	}

	text := renderFrontmatter(original)
	parsed, err := parseFrontmatter(text)
	if err != nil {
		t.Fatalf("parseFrontmatter failed on rendered output: %v", err)
	}

	if parsed.Name != original.Name {
		t.Errorf("Name: got %q, want %q", parsed.Name, original.Name)
	}
	if parsed.Description != original.Description {
		t.Errorf("Description: got %q, want %q", parsed.Description, original.Description)
	}
	if parsed.Type != original.Type {
		t.Errorf("Type: got %q, want %q", parsed.Type, original.Type)
	}
	if parsed.Content != original.Content {
		t.Errorf("Content: got %q, want %q", parsed.Content, original.Content)
	}
}

// ---------------------------------------------------------------------------
// slugify tests
// ---------------------------------------------------------------------------

func TestSlugify_Basic(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello_world"},
		{"prefer-pnpm", "prefer_pnpm"},
		{"test@example", "test_example"},
		{"UPPERCASE", "uppercase"},
		{"  spaces  ", "spaces"},
		{"a-b_c!d", "a_b_c_d"},
		{"", "memory"}, // empty string becomes "memory"
		{"123", "123"},
	}

	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.Delete tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Delete_Success(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("to_delete", "Will be deleted", MemTypeUser, "content.")

	if mm.Count() != 1 {
		t.Fatalf("expected 1 entry before delete, got %d", mm.Count())
	}

	err := mm.Delete("to_delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if mm.Count() != 0 {
		t.Errorf("expected 0 entries after delete, got %d", mm.Count())
	}

	// File should be removed from disk
	expectedFile := filepath.Join(dir, ".iroha", "memory", "to_delete.md")
	if _, err := os.Stat(expectedFile); !os.IsNotExist(err) {
		t.Error("expected file to be deleted from disk")
	}
}

func TestMemoryManager_Delete_NotFound(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	err := mm.Delete("nonexistent")
	if err == nil {
		t.Error("expected error when deleting nonexistent entry")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestMemoryManager_Delete_DoesNotAffectOtherEntries(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	_ = mm.Save("keep_this", "Should remain", MemTypeUser, "content.")
	_ = mm.Save("delete_this", "Should be removed", MemTypeFeedback, "content.")

	err := mm.Delete("delete_this")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if mm.Count() != 1 {
		t.Errorf("expected 1 entry after deleting one, got %d", mm.Count())
	}

	entries := mm.List()
	if len(entries[MemTypeUser]) != 1 {
		t.Error("expected user entry to survive")
	}
	if len(entries[MemTypeFeedback]) != 0 {
		t.Error("expected feedback entry to be gone")
	}
}

// ---------------------------------------------------------------------------
// MemoryManager.Save cap tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Save_CapEnforced(t *testing.T) {
	dir := t.TempDir()
	mm := newMemoryManagerInDir(t, dir)

	// Fill up to MaxMemoryEntries
	for i := 0; i < MaxMemoryEntries; i++ {
		name := fmt.Sprintf("entry_%d", i)
		err := mm.Save(name, "desc", MemTypeUser, "content.")
		if err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	// Next save should fail
	err := mm.Save("overflow", "desc", MemTypeUser, "content.")
	if err == nil {
		t.Error("expected error when exceeding max entries")
	}
	if !errors.Is(err, fmt.Errorf("memory store full: max %d entries reached", MaxMemoryEntries)) {
		// At least check it mentions full or cap
		if !strings.Contains(err.Error(), "full") && !strings.Contains(err.Error(), "max") {
			t.Errorf("expected capacity error, got: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// estimateTokens edge cases (from session_store_helpers.go)
// ---------------------------------------------------------------------------

func TestEstimateTokens_Zero(t *testing.T) {
	if estimateTokens(0) != 0 {
		t.Error("expected 0 tokens for length 0")
	}
}

func TestEstimateTokens_Negative(t *testing.T) {
	if estimateTokens(-1) != 0 {
		t.Error("expected 0 tokens for negative length")
	}
}

func TestEstimateTokens_Small(t *testing.T) {
	if estimateTokens(3) != 0 {
		t.Errorf("expected 0 tokens for 3 bytes (3/4=0), got %d", estimateTokens(3))
	}
}

func TestEstimateTokens_Large(t *testing.T) {
	if estimateTokens(4000) != 1000 {
		t.Errorf("expected 1000 tokens for 4000 bytes, got %d", estimateTokens(4000))
	}
}
