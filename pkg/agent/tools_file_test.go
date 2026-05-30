package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileWriteHandler(t *testing.T) {
	workspace, err := os.MkdirTemp("", "iroha-file-write-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)

	stdCtx := context.WithValue(context.Background(), WorkdirKey, workspace)
	ctx := &mockToolContext{Context: stdCtx}

	// 1. Test basic write
	filePath := filepath.Join(workspace, "test1.txt")
	relPath := "test1.txt"
	args := FileWriteArgs{
		Path:    relPath,
		Content: "hello world\nline 2",
	}

	res, err := FileWriteHandler(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\nline 2" {
		t.Errorf("expected 'hello world\\nline 2', got %q", string(data))
	}

	// 2. Test auto-creating parent directories
	subPath := "sub/dir/test2.txt"
	args2 := FileWriteArgs{
		Path:    subPath,
		Content: "nested text",
	}
	res2, err := FileWriteHandler(ctx, args2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res2.Success {
		t.Error("expected Success to be true")
	}

	data2, err := os.ReadFile(filepath.Join(workspace, "sub", "dir", "test2.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data2) != "nested text" {
		t.Errorf("expected 'nested text', got %q", string(data2))
	}

	// 3. Sandbox check
	escapedArgs := FileWriteArgs{
		Path:    "../outside.txt",
		Content: "escaped content",
	}
	_, err = FileWriteHandler(ctx, escapedArgs)
	if err == nil {
		t.Error("expected sandbox escape to fail, got nil error")
	} else if !strings.Contains(err.Error(), "security sandbox blocked") {
		t.Errorf("expected sandbox block error, got: %v", err)
	}
}

func TestFileReadHandler(t *testing.T) {
	workspace, err := os.MkdirTemp("", "iroha-file-read-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)

	stdCtx := context.WithValue(context.Background(), WorkdirKey, workspace)
	ctx := &mockToolContext{Context: stdCtx}

	// Create a test file
	filePath := filepath.Join(workspace, "test.txt")
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Read entire file
	res, err := FileReadHandler(ctx, FileReadArgs{Path: "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Content != content {
		t.Errorf("expected %q, got %q", content, res.Content)
	}

	// 2. Read specific lines
	res2, err := FileReadHandler(ctx, FileReadArgs{
		Path:      "test.txt",
		StartLine: 2,
		EndLine:   4,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedLines := "Lines 2-4 of 5\n2\tLine 2\n3\tLine 3\n4\tLine 4\n"
	if res2.Content != expectedLines {
		t.Errorf("expected %q, got %q", expectedLines, res2.Content)
	}

	// 3. Read directory - should fail
	subDir := filepath.Join(workspace, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = FileReadHandler(ctx, FileReadArgs{Path: "sub"})
	if err == nil {
		t.Error("expected reading a directory to fail")
	} else if !strings.Contains(err.Error(), "is a directory, not a file") {
		t.Errorf("unexpected error for directory read: %v", err)
	}

	// 4. Non-existent file - should fail with self-repair suggestion
	_, err = FileReadHandler(ctx, FileReadArgs{Path: "nonexistent.txt"})
	if err == nil {
		t.Error("expected reading non-existent file to fail")
	} else if !strings.Contains(err.Error(), "[Self-repair suggestion]") {
		t.Errorf("expected self-repair suggestion in error, got: %v", err)
	}

	// 5. Check size limit (>10MB)
	bigFilePath := filepath.Join(workspace, "big.txt")
	f, err := os.Create(bigFilePath)
	if err != nil {
		t.Fatal(err)
	}
	// Truncate creates a sparse file instantly on most OSs
	if err := f.Truncate(maxFileReadSize + 1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	_, err = FileReadHandler(ctx, FileReadArgs{Path: "big.txt"})
	if err == nil {
		t.Error("expected reading file exceeding 10MB to fail")
	} else if !strings.Contains(err.Error(), "exceeding the 10MB read limit") {
		t.Errorf("unexpected error for large file: %v", err)
	}

	// 6. Sandbox validation check
	_, err = FileReadHandler(ctx, FileReadArgs{Path: "../escaped.txt"})
	if err == nil {
		t.Error("expected sandbox escape to fail")
	} else if !strings.Contains(err.Error(), "security sandbox blocked") {
		t.Errorf("expected security sandbox blocked, got: %v", err)
	}
}

func TestFileEditHandler(t *testing.T) {
	workspace, err := os.MkdirTemp("", "iroha-file-edit-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)

	stdCtx := context.WithValue(context.Background(), WorkdirKey, workspace)
	ctx := &mockToolContext{Context: stdCtx}

	filePath := filepath.Join(workspace, "edit.txt")
	content := "orange\nbanana\napple\nbanana\ncherry"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Dry run - should generate diff but not write changes
	resDry, err := FileEditHandler(ctx, FileEditArgs{
		Path:      "edit.txt",
		OldString: "apple",
		NewString: "peach",
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resDry.Success {
		t.Error("expected dry-run success")
	}
	if !strings.Contains(resDry.Diff, "-apple") || !strings.Contains(resDry.Diff, "+peach") {
		t.Errorf("unexpected dry-run diff:\n%s", resDry.Diff)
	}
	// Verify file was NOT modified
	data, _ := os.ReadFile(filePath)
	if string(data) != content {
		t.Error("file was modified during dry run")
	}

	// 2. Exact match with multiple occurrences and ReplaceAll = false - should fail
	_, err = FileEditHandler(ctx, FileEditArgs{
		Path:      "edit.txt",
		OldString: "banana",
		NewString: "grape",
	})
	if err == nil {
		t.Error("expected error for multiple matches without ReplaceAll")
	} else if !strings.Contains(err.Error(), "matches 2 times") {
		t.Errorf("unexpected error: %v", err)
	}

	// 3. Exact match with multiple occurrences and ReplaceAll = true - should succeed
	resAll, err := FileEditHandler(ctx, FileEditArgs{
		Path:       "edit.txt",
		OldString:  "banana",
		NewString:  "grape",
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resAll.Success {
		t.Error("expected success")
	}
	dataAll, _ := os.ReadFile(filePath)
	expectedContent := "orange\ngrape\napple\ngrape\ncherry"
	if string(dataAll) != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, string(dataAll))
	}

	// 4. Exact match (first only) when unique - should succeed
	resFirst, err := FileEditHandler(ctx, FileEditArgs{
		Path:      "edit.txt",
		OldString: "apple",
		NewString: "peach",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resFirst.Success {
		t.Error("expected success")
	}
	dataFirst, _ := os.ReadFile(filePath)
	expectedContent2 := "orange\ngrape\npeach\ngrape\ncherry"
	if string(dataFirst) != expectedContent2 {
		t.Errorf("expected content %q, got %q", expectedContent2, string(dataFirst))
	}

	// 5. Whitespace tolerant fallback match
	// Reset content
	_ = os.WriteFile(filePath, []byte("func   Foo( x  int ) {\n\treturn\n}"), 0644)
	resWS, err := FileEditHandler(ctx, FileEditArgs{
		Path:      "edit.txt",
		OldString: "func Foo(x int) {\n\treturn\n}",
		NewString: "func Bar() {}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resWS.Success {
		t.Error("expected success")
	}
	dataWS, _ := os.ReadFile(filePath)
	if string(dataWS) != "func Bar() {}" {
		t.Errorf("expected whitespace tolerant edit to replace, got: %q", string(dataWS))
	}

	// 6. Old string not found - should fail
	_, err = FileEditHandler(ctx, FileEditArgs{
		Path:      "edit.txt",
		OldString: "nonexistent",
		NewString: "exists",
	})
	if err == nil {
		t.Error("expected error for nonexistent old string")
	} else if !strings.Contains(err.Error(), "old_string not found in file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileEditBatchHandler(t *testing.T) {
	workspace, err := os.MkdirTemp("", "iroha-file-edit-batch-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)

	stdCtx := context.WithValue(context.Background(), WorkdirKey, workspace)
	ctx := &mockToolContext{Context: stdCtx}

	file1 := filepath.Join(workspace, "file1.txt")
	file2 := filepath.Join(workspace, "file2.txt")

	_ = os.WriteFile(file1, []byte("apple\nbanana"), 0644)
	_ = os.WriteFile(file2, []byte("orange\ncherry"), 0644)

	// 1. Success batch edit
	res, err := FileEditBatchHandler(ctx, FileEditBatchArgs{
		Edits: []FileEditArgs{
			{Path: "file1.txt", OldString: "apple", NewString: "apricot"},
			{Path: "file2.txt", OldString: "orange", NewString: "grapefruit"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Error("expected batch success")
	}

	data1, _ := os.ReadFile(file1)
	data2, _ := os.ReadFile(file2)
	if string(data1) != "apricot\nbanana" {
		t.Errorf("file1 not updated, got: %q", string(data1))
	}
	if string(data2) != "grapefruit\ncherry" {
		t.Errorf("file2 not updated, got: %q", string(data2))
	}

	// 2. Rollback verification: if any edit in batch fails, all changes must be rolled back!
	// Reset contents first
	_ = os.WriteFile(file1, []byte("apple\nbanana"), 0644)
	_ = os.WriteFile(file2, []byte("orange\ncherry"), 0644)

	_, err = FileEditBatchHandler(ctx, FileEditBatchArgs{
		Edits: []FileEditArgs{
			{Path: "file1.txt", OldString: "apple", NewString: "apricot"},
			// This second edit will fail since "pear" is not in file2.txt
			{Path: "file2.txt", OldString: "pear", NewString: "grapefruit"},
		},
	})
	if err == nil {
		t.Error("expected batch edit to fail because of failed second edit")
	}

	// Verify that file1 was rolled back to "apple\nbanana" and NOT left as "apricot\nbanana"
	data1Rollback, _ := os.ReadFile(file1)
	if string(data1Rollback) != "apple\nbanana" {
		t.Errorf("expected file1 to be rolled back to original content, but got: %q", string(data1Rollback))
	}

	data2Rollback, _ := os.ReadFile(file2)
	if string(data2Rollback) != "orange\ncherry" {
		t.Errorf("expected file2 to remain unmodified, but got: %q", string(data2Rollback))
	}
}
