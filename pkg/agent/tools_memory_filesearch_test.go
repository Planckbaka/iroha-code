package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)


func TestMemoryUpdateHandler_Success(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	// Save first, then update via handler
	MemorySaveHandler(nil, MemorySaveArgs{Name: "up_test", Description: "old desc", Type: "user", Content: "old content"})

	res, err := MemoryUpdateHandler(nil, MemoryUpdateArgs{Name: "up_test", Description: "new desc", Type: "user", Content: "new content"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK=true, got false: %s", res.Message)
	}
	if res.Message != "Memory updated: up_test" {
		t.Errorf("unexpected message: %q", res.Message)
	}
}

func TestMemoryUpdateHandler_NotFound(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	res, err := MemoryUpdateHandler(nil, MemoryUpdateArgs{Name: "nonexistent", Description: "x", Type: "user", Content: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false for nonexistent entry")
	}
	if res.Message == "" {
		t.Error("expected error message")
	}
}

func TestMemoryUpdateHandler_InvalidType(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	MemorySaveHandler(nil, MemorySaveArgs{Name: "type_test", Description: "d", Type: "user", Content: "c"})

	res, err := MemoryUpdateHandler(nil, MemoryUpdateArgs{Name: "type_test", Description: "d", Type: "invalid_type", Content: "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false for invalid type")
	}
}


func TestMemoryDeleteHandler_Success(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	MemorySaveHandler(nil, MemorySaveArgs{Name: "del_test", Description: "to delete", Type: "feedback", Content: "will be removed"})

	res, err := MemoryDeleteHandler(nil, MemoryDeleteArgs{Name: "del_test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK=true, got false: %s", res.Message)
	}
	if res.Message != "Memory deleted: del_test" {
		t.Errorf("unexpected message: %q", res.Message)
	}

	// Verify entry is gone
	if GlobalMemoryManager.Count() != 0 {
		t.Errorf("expected 0 entries after delete, got %d", GlobalMemoryManager.Count())
	}
}

func TestMemoryDeleteHandler_NotFound(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	res, err := MemoryDeleteHandler(nil, MemoryDeleteArgs{Name: "no_such_entry"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false for nonexistent entry")
	}
}

func TestMemoryDeleteHandler_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origMM := GlobalMemoryManager
	GlobalMemoryManager = NewMemoryManager()
	t.Cleanup(func() { GlobalMemoryManager = origMM })

	MemorySaveHandler(nil, MemorySaveArgs{Name: "file_del", Description: "d", Type: "project", Content: "c"})

	memFile := filepath.Join(dir, ".iroha", "memory", "file_del.md")
	if _, err := os.Stat(memFile); err != nil {
		t.Fatalf("memory file should exist before delete: %v", err)
	}

	MemoryDeleteHandler(nil, MemoryDeleteArgs{Name: "file_del"})

	if _, err := os.Stat(memFile); !os.IsNotExist(err) {
		t.Error("memory file should be removed after delete")
	}
}


// setupFileTestDir creates a temp dir, changes CWD to it, and disables sandbox.
// Returns the temp dir path. Cleanup restores original state.
func setupFileTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	origSandbox := GlobalSandboxEnabled
	GlobalSandboxEnabled = false
	t.Cleanup(func() { GlobalSandboxEnabled = origSandbox })

	return dir
}

func TestListDirHandler_BasicListing(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main"), 0644)

	res, err := ListDirHandler(nil, ListDirArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Entries) == 0 {
		t.Fatal("expected at least one entry")
	}

	found := map[string]bool{}
	for _, e := range res.Entries {
		found[e] = true
	}
	if !found["a.txt"] {
		t.Error("expected a.txt in listing")
	}
	if !found["b.go"] {
		t.Error("expected b.go in listing")
	}
	if !found["subdir/"] {
		t.Error("expected subdir/ in listing (directory should have trailing /)")
	}
}

func TestListDirHandler_DefaultPath(t *testing.T) {
	dir := setupFileTestDir(t)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0644)

	// Empty path should default to "." which resolves to CWD (the temp dir)
	res, err := ListDirHandler(nil, ListDirArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0] != "file.txt" {
		t.Errorf("expected [file.txt], got %v", res.Entries)
	}
}

func TestListDirHandler_MaxDepthClamping(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "deep.txt"), []byte("deep"), 0644)

	// depth 1: should not see deeply nested files
	res, err := ListDirHandler(nil, ListDirArgs{Path: dir, MaxDepth: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range res.Entries {
		if e == "a/b/c/deep.txt" {
			t.Error("depth 1 should not see deeply nested file")
		}
	}

	// depth 4: should see the deep file
	res2, err := ListDirHandler(nil, ListDirArgs{Path: dir, MaxDepth: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, e := range res2.Entries {
		if e == "a/b/c/deep.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("depth 4 should see deep file, got entries: %v", res2.Entries)
	}
}

func TestListDirHandler_ExcludedDirs(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "objects", "pack.txt"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("x"), 0644)

	res, err := ListDirHandler(nil, ListDirArgs{Path: dir, MaxDepth: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range res.Entries {
		if e == ".git/" || e == ".git/objects/" {
			t.Errorf("excluded dir .git should not appear in listing, got: %s", e)
		}
	}
}

func TestListDirHandler_EntryLimit(t *testing.T) {
	dir := setupFileTestDir(t)
	// Create 250 files -- only 200 should be returned
	for i := 0; i < 250; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file_%03d.txt", i))
		os.WriteFile(name, []byte("x"), 0644)
	}

	res, err := ListDirHandler(nil, ListDirArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Entries) > 200 {
		t.Errorf("expected at most 200 entries, got %d", len(res.Entries))
	}
}

func TestListDirHandler_DepthZeroDefaultsToOne(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "inner"), 0755)
	os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "inner", "nested.txt"), []byte("x"), 0644)

	// MaxDepth=0 should be treated as 1
	res, err := ListDirHandler(nil, ListDirArgs{Path: dir, MaxDepth: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range res.Entries {
		if e == "inner/nested.txt" {
			t.Error("MaxDepth=0 (clamped to 1) should not see nested files")
		}
	}
}

func TestListDirHandler_MaxDepthOver4(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d"), 0755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "d", "deep.txt"), []byte("x"), 0644)

	// MaxDepth=10 should be clamped to 4
	res, err := ListDirHandler(nil, ListDirArgs{Path: dir, MaxDepth: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range res.Entries {
		if e == "a/b/c/d/deep.txt" {
			t.Error("MaxDepth=10 should be clamped to 4; should not see depth-5 file")
		}
	}
}

func TestListDirHandler_EmptyDir(t *testing.T) {
	dir := setupFileTestDir(t)

	res, err := ListDirHandler(nil, ListDirArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Entries) != 0 {
		t.Errorf("expected 0 entries for empty dir, got %d", len(res.Entries))
	}
}


func TestFindHandler_BasicGlob(t *testing.T) {
	dir := setupFileTestDir(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# test"), 0644)
	os.MkdirAll(filepath.Join(dir, "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "pkg", "handler.go"), []byte("package pkg"), 0644)

	res, err := FindHandler(nil, FindArgs{Pattern: "*.go", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range res.Files {
		found[f] = true
	}
	if !found["main.go"] {
		t.Error("expected main.go in results")
	}
	if found["readme.md"] {
		t.Error("did not expect readme.md for *.go pattern")
	}
	if res.Total != len(res.Files) {
		t.Errorf("Total (%d) should match len(Files) (%d)", res.Total, len(res.Files))
	}
}

func TestFindHandler_Globstar(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "src", "util"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "app.ts"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "util", "helper.ts"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0644)

	res, err := FindHandler(nil, FindArgs{Pattern: "**/*.ts", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range res.Files {
		found[f] = true
	}
	if !found["src/app.ts"] {
		t.Error("expected src/app.ts in results")
	}
	if !found["src/util/helper.ts"] {
		t.Error("expected src/util/helper.ts in results")
	}
	if found["root.txt"] {
		t.Error("did not expect root.txt for **/*.ts pattern")
	}
}

func TestFindHandler_DefaultPath(t *testing.T) {
	dir := setupFileTestDir(t)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("x"), 0644)

	// Empty Path defaults to "."
	res, err := FindHandler(nil, FindArgs{Pattern: "*.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 1 || res.Files[0] != "test.txt" {
		t.Errorf("expected [test.txt], got %v", res.Files)
	}
}

func TestFindHandler_NoMatches(t *testing.T) {
	dir := setupFileTestDir(t)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0644)

	res, err := FindHandler(nil, FindArgs{Pattern: "*.py", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("expected 0 matches, got %d", res.Total)
	}
}

func TestFindHandler_ExcludedDirs(t *testing.T) {
	dir := setupFileTestDir(t)
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("x"), 0644)

	res, err := FindHandler(nil, FindArgs{Pattern: "**/*.js", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range res.Files {
		if f == "node_modules/pkg/index.js" {
			t.Error("node_modules files should be excluded")
		}
	}
	if res.Total != 1 || res.Files[0] != "app.js" {
		t.Errorf("expected only app.js, got %v", res.Files)
	}
}

func TestFindHandler_ResultsAreSorted(t *testing.T) {
	dir := setupFileTestDir(t)
	for _, name := range []string{"c.go", "a.go", "b.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)
	}

	res, err := FindHandler(nil, FindArgs{Pattern: "*.go", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"a.go", "b.go", "c.go"}
	if len(res.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(res.Files))
	}
	for i, exp := range expected {
		if res.Files[i] != exp {
			t.Errorf("res.Files[%d] = %q, want %q", i, res.Files[i], exp)
		}
	}
}

func TestFindHandler_EntryLimit(t *testing.T) {
	dir := setupFileTestDir(t)
	// Create 150 files -- only 100 should be returned
	for i := 0; i < 150; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%03d.txt", i)), []byte("x"), 0644)
	}

	res, err := FindHandler(nil, FindArgs{Pattern: "*.txt", Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Files) > 100 {
		t.Errorf("expected at most 100 files, got %d", len(res.Files))
	}
}
