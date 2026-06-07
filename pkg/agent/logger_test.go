package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "OpenAI API Key",
			input:    "my api key is sk-abcdefghijklmnopqrstuvwxyz0123456789",
			expected: "my api key is [REDACTED]",
		},
		{
			name:     "Bearer Token",
			input:    "Authorization: bearer abc123XYZ_-.~something",
			expected: "Authorization: Bearer [REDACTED]",
		},
		{
			name:     "JSON API Key field",
			input:    `{"api_key": "sensitive-value-here", "status": "ok"}`,
			expected: `{"api_key":"[REDACTED]", "status": "ok"}`,
		},
		{
			name:     "JSON Token field",
			input:    `{"token": "someSecretToken", "user": "admin"}`,
			expected: `{"token":"[REDACTED]", "user": "admin"}`,
		},
		{
			name:     "URL Query or Env Equal Sign Key",
			input:    "api_key=mysecretkey&other=val",
			expected: "api_key=[REDACTED]&other=val",
		},
		{
			name:     "URL Query Token",
			input:    "token=my_secret_token_123",
			expected: "token=[REDACTED]",
		},
		{
			name:     "Plain Text Key Value",
			input:    "secret=supersecurepassword123",
			expected: "secret=[REDACTED]",
		},
		{
			name:     "Google/Gemini API Key",
			input:    "key=AIzaSyABCdefGHIjklMNOpqrsTUVwxyz1234567890",
			expected: "key=[REDACTED]",
		},
		{
			name:     "Anthropic API Key with sk-ant- prefix",
			input:    "using key sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456",
			expected: "using key [REDACTED]",
		},
		{
			name:     "x-api-key header",
			input:    "x-api-key: sk-ant-api03-my-anthropic-key-here12345",
			expected: "x-api-key: [REDACTED]",
		},
		{
			name:     "JSON api-key field with hyphen",
			input:    `{"api-key": "sensitive-value-here", "status": "ok"}`,
			expected: `{"api-key":"[REDACTED]", "status": "ok"}`,
		},
		{
			name:     "env-style api-key assignment",
			input:    "api-key=my-secret-key-12345",
			expected: "api-key=[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if got != tt.expected {
				t.Errorf("RedactSecrets() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestLoggerManager_ConcurrentAndJSONL(t *testing.T) {
	// Create a temporary directory for test logs
	tempDir, err := os.MkdirTemp("", "iroha_logger_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	lm := &LoggerManager{
		logsDir: tempDir,
	}
	lm.SetSessionID("test_session_123")

	const (
		goroutines = 10
		logsPerGo  = 20
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < logsPerGo; j++ {
				metadata := map[string]any{
					"worker_id": id,
					"loop_idx":  j,
					"api_key":   "sk-testkey12345678901234567890", // should be redacted
				}
				lm.Log(LevelInfo, CatSystem, "test_event", fmt.Sprintf("message from %d-%d", id, j), 15, metadata)
			}
		}(i)
	}

	wg.Wait()

	// Ensure files are closed to flush buffers
	lm.SetSessionID("")

	// Verify JSONL File
	jsonlPath := filepath.Join(tempDir, "session_test_session_123_audit.jsonl")
	jFile, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("failed to open jsonl audit file: %v", err)
	}
	defer jFile.Close()

	scanner := bufio.NewScanner(jFile)
	logCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		logCount++

		// Verify redact worked on JSONL stream-level outcome
		if strings.Contains(line, "sk-testkey12345678901234567890") {
			t.Errorf("unredacted API key found in line: %s", line)
		}

		var record AuditLogRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("failed to unmarshal JSONL record: %v, line: %s", err, line)
		}

		if record.SessionID != "test_session_123" {
			t.Errorf("expected session_id 'test_session_123', got %q", record.SessionID)
		}
		if record.Level != LevelInfo {
			t.Errorf("expected level 'INFO', got %q", record.Level)
		}
		if record.Category != CatSystem {
			t.Errorf("expected category 'system', got %q", record.Category)
		}
		if record.Event != "test_event" {
			t.Errorf("expected event 'test_event', got %q", record.Event)
		}
		if record.DurationMS != 15 {
			t.Errorf("expected duration_ms 15, got %d", record.DurationMS)
		}

		// Check redacted metadata
		workerVal, hasWorker := record.Metadata["worker_id"]
		if !hasWorker {
			t.Errorf("missing 'worker_id' in metadata")
		} else {
			if _, ok := workerVal.(float64); !ok {
				t.Errorf("worker_id is not a float64 (unmarshalled from JSON)")
			}
		}

		apiKeyVal, hasKey := record.Metadata["api_key"]
		if !hasKey {
			t.Errorf("missing 'api_key' in metadata")
		} else {
			if apiKeyVal != "[REDACTED]" {
				t.Errorf("expected redacted api_key, got %v", apiKeyVal)
			}
		}
	}

	expectedLogs := goroutines * logsPerGo
	if logCount != expectedLogs {
		t.Errorf("expected %d logs, got %d", expectedLogs, logCount)
	}

	// Verify Plain-text Log File
	plainPath := filepath.Join(tempDir, "session_test_session_123_audit.log")
	pFile, err := os.Open(plainPath)
	if err != nil {
		t.Fatalf("failed to open plain audit file: %v", err)
	}
	defer pFile.Close()

	pScanner := bufio.NewScanner(pFile)
	plainLogCount := 0
	for pScanner.Scan() {
		line := pScanner.Text()
		plainLogCount++

		if strings.Contains(line, "sk-testkey12345678901234567890") {
			t.Errorf("unredacted API key found in plain log line: %s", line)
		}
		if !strings.Contains(line, "[INFO]") || !strings.Contains(line, "[system]") || !strings.Contains(line, "test_event") {
			t.Errorf("plain text log line structured incorrectly: %s", line)
		}
		if !strings.Contains(line, `duration=15ms`) {
			t.Errorf("plain text log line missing duration: %s", line)
		}
	}

	if plainLogCount != expectedLogs {
		t.Errorf("expected %d plain logs, got %d", expectedLogs, plainLogCount)
	}
}

func TestLoggerManager_LogWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "iroha_logwrite_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	lm := &LoggerManager{
		logsDir: tempDir,
	}
	lm.SetSessionID("logwrite_sess")

	event := AuditEvent{
		Level:      "AUDIT",
		Category:   "security_gate",
		Event:      "sandbox_allowed",
		Message:    "Accessed path within sandbox bounds",
		DurationMS: 4,
		Metadata: map[string]any{
			"path":    "/tmp/workspace/file.txt",
			"api_key": "sk-someapi-key-here12345",
		},
	}

	lm.LogWrite(event)
	lm.SetSessionID("") // flush and close files

	// Verify JSONL
	jsonlPath := filepath.Join(tempDir, "session_logwrite_sess_audit.jsonl")
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to read jsonl log: %v", err)
	}

	var parsed AuditEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSONL: %v", err)
	}

	if parsed.Level != "AUDIT" || parsed.Category != "security_gate" || parsed.Event != "sandbox_allowed" {
		t.Errorf("unexpected event content: %+v", parsed)
	}

	if parsed.Metadata["api_key"] != "[REDACTED]" {
		t.Errorf("expected redacted api_key, got: %v", parsed.Metadata["api_key"])
	}
}

func TestLoggerManager_LogRunEventWritesReplayableSequence(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("run-events")

	lm.LogRunEvent(RunEvent{RunID: "run-1", Sequence: 1, Type: "run.accepted"})
	lm.LogRunEvent(RunEvent{RunID: "run-1", Sequence: 2, Type: "run.completed"})

	data, err := os.ReadFile(filepath.Join(tempDir, "run-run-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d run events, want 2", len(lines))
	}

	var first, second RunEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first.SchemaVersion != 1 || first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("unexpected replay sequence: first=%+v second=%+v", first, second)
	}
}

// ---------------------------------------------------------------------------
// CurrentSessionID coverage
// ---------------------------------------------------------------------------

func TestLoggerManager_CurrentSessionID(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}

	// Initially empty
	if sid := lm.CurrentSessionID(); sid != "" {
		t.Errorf("Expected empty session ID initially, got %q", sid)
	}

	// After setting
	lm.SetSessionID("my-session")
	if sid := lm.CurrentSessionID(); sid != "my-session" {
		t.Errorf("Expected 'my-session', got %q", sid)
	}

	// After clearing
	lm.SetSessionID("")
	if sid := lm.CurrentSessionID(); sid != "" {
		t.Errorf("Expected empty after clear, got %q", sid)
	}
}

// ---------------------------------------------------------------------------
// LogWrite package-level function (0% coverage)
// ---------------------------------------------------------------------------

func TestLogWrite_PackageLevel(t *testing.T) {
	tempDir := t.TempDir()

	// Swap the global logger to use our temp dir
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	GlobalLogger.SetSessionID("pkglevel-test")

	event := AuditEvent{
		Level:    "INFO",
		Category: "system",
		Event:    "pkg_level_test",
		Message:  "package-level LogWrite test",
	}
	LogWrite(event)

	// Flush
	GlobalLogger.SetSessionID("")

	// Verify the event was written
	data, err := os.ReadFile(filepath.Join(tempDir, "session_pkglevel-test_audit.jsonl"))
	if err != nil {
		t.Fatalf("failed to read jsonl: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSONL log data")
	}

	var parsed AuditEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if parsed.Event != "pkg_level_test" {
		t.Errorf("expected event 'pkg_level_test', got %q", parsed.Event)
	}
}

// ---------------------------------------------------------------------------
// LogWrite lazy init (sessionID == "" path)
// ---------------------------------------------------------------------------

func TestLogWrite_LazyInit(t *testing.T) {
	tempDir := t.TempDir()

	lm := &LoggerManager{logsDir: tempDir}
	// Don't call SetSessionID — files are nil, sessionID is empty

	// LogWrite should lazy-init with "uninitialized" session
	event := AuditEvent{
		Level:    "WARN",
		Category: "system",
		Event:    "lazy_init",
		Message:  "lazy initialization test",
	}
	lm.LogWrite(event)

	// Verify it created files with "uninitialized" session
	jsonlPath := filepath.Join(tempDir, "session_uninitialized_audit.jsonl")
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to read lazy-init jsonl: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty lazy-init JSONL data")
	}
}

// ---------------------------------------------------------------------------
// ReadTraceTail coverage (0%)
// ---------------------------------------------------------------------------

func TestReadTraceTail(t *testing.T) {
	tempDir := t.TempDir()

	// Swap global logger to use temp dir
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	GlobalLogger.SetSessionID("trace-test")

	// Write some traces
	LogToolTrace("tool_a", map[string]any{"arg": "val"}, "ok", 100)
	LogToolTrace("tool_b", map[string]any{"arg": "val2"}, "error", 200)
	LogToolTrace("tool_c", map[string]any{"arg": "val3"}, "ok", 300)

	// Read the last 2
	traces, err := ReadTraceTail("trace-test", 2)
	if err != nil {
		t.Fatalf("ReadTraceTail failed: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("Expected 2 traces, got %d", len(traces))
	}
	if traces[0].Tool != "tool_b" {
		t.Errorf("Expected first trace tool 'tool_b', got %q", traces[0].Tool)
	}
	if traces[1].Tool != "tool_c" {
		t.Errorf("Expected second trace tool 'tool_c', got %q", traces[1].Tool)
	}
}

func TestReadTraceTail_ReadAll(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	GlobalLogger.SetSessionID("trace-all")

	LogToolTrace("tool_a", nil, "ok", 10)
	LogToolTrace("tool_b", nil, "ok", 20)

	// Request more lines than exist
	traces, err := ReadTraceTail("trace-all", 100)
	if err != nil {
		t.Fatalf("ReadTraceTail failed: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("Expected 2 traces, got %d", len(traces))
	}
}

func TestReadTraceTail_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	_, err := ReadTraceTail("nonexistent-session", 5)
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestReadTraceTail_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	// Create an empty trace file
	tracePath := filepath.Join(tempDir, "trace-empty-sess.jsonl")
	if err := os.WriteFile(tracePath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Empty file returns empty traces slice (no error) because
	// strings.Split of empty string produces [""], which has len 1,
	// but the line fails json.Unmarshal and is skipped.
	traces, err := ReadTraceTail("empty-sess", 5)
	if err != nil {
		t.Errorf("Unexpected error for empty trace file: %v", err)
	}
	if len(traces) != 0 {
		t.Errorf("Expected 0 traces from empty file, got %d", len(traces))
	}
}

// ---------------------------------------------------------------------------
// LogToolTrace session switch
// ---------------------------------------------------------------------------

func TestLogToolTrace_SessionSwitch(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	// Reset trace logger state
	origTrace := globalTraceLogger
	globalTraceLogger = &traceLogger{}
	defer func() { globalTraceLogger = origTrace }()

	// First session
	GlobalLogger.SetSessionID("switch-1")
	LogToolTrace("tool_x", nil, "ok", 10)

	// Switch session
	GlobalLogger.SetSessionID("switch-2")
	LogToolTrace("tool_y", nil, "ok", 20)

	// Verify first session file
	data1, err := os.ReadFile(filepath.Join(tempDir, "trace-switch-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data1) == 0 {
		t.Fatal("Expected trace data for switch-1")
	}

	// Verify second session file
	data2, err := os.ReadFile(filepath.Join(tempDir, "trace-switch-2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data2) == 0 {
		t.Fatal("Expected trace data for switch-2")
	}
}

// ---------------------------------------------------------------------------
// LogToolTrace uninitialized session
// ---------------------------------------------------------------------------

func TestLogToolTrace_UninitializedSession(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	// Reset trace logger state
	origTrace := globalTraceLogger
	globalTraceLogger = &traceLogger{}
	defer func() { globalTraceLogger = origTrace }()

	// Don't set session ID — should use "uninitialized"
	LogToolTrace("tool_u", nil, "ok", 5)

	tracePath := filepath.Join(tempDir, "trace-uninitialized.jsonl")
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("Expected trace file for uninitialized session: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected trace data for uninitialized session")
	}
}

// ---------------------------------------------------------------------------
// cleanupOldTraceFiles coverage
// ---------------------------------------------------------------------------

func TestCleanupOldTraceFiles(t *testing.T) {
	tempDir := t.TempDir()

	// Create an old trace file (mod time set to 8 days ago)
	oldPath := filepath.Join(tempDir, "trace-old-session.jsonl")
	if err := os.WriteFile(oldPath, []byte(`{"tool":"old"}\n`), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().AddDate(0, 0, -8)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a recent trace file
	recentPath := filepath.Join(tempDir, "trace-recent-session.jsonl")
	if err := os.WriteFile(recentPath, []byte(`{"tool":"recent"}\n`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-trace file that should be ignored
	otherPath := filepath.Join(tempDir, "other-file.jsonl")
	if err := os.WriteFile(otherPath, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}
	otherTime := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(otherPath, otherTime, otherTime); err != nil {
		t.Fatal(err)
	}

	// Run cleanup
	cleanupOldTraceFiles(tempDir)

	// Old trace file should be removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("Expected old trace file to be cleaned up")
	}

	// Recent trace file should still exist
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Error("Expected recent trace file to still exist")
	}

	// Non-trace file should still exist
	if _, err := os.Stat(otherPath); os.IsNotExist(err) {
		t.Error("Expected non-trace file to still exist (not a trace file)")
	}
}

// ---------------------------------------------------------------------------
// LogRunEvent with empty sessionID
// ---------------------------------------------------------------------------

func TestLogRunEvent_EmptySessionID(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	// No SetSessionID called

	lm.LogRunEvent(RunEvent{RunID: "run-empty", Sequence: 1, Type: "test"})

	// Should create run-uninitialized.jsonl
	data, err := os.ReadFile(filepath.Join(tempDir, "run-uninitialized.jsonl"))
	if err != nil {
		t.Fatalf("Expected run file for uninitialized session: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected run event data")
	}
}

// ---------------------------------------------------------------------------
// LogRunEvent redacts secrets
// ---------------------------------------------------------------------------

func TestLogRunEvent_RedactsSecrets(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("run-redact")

	lm.LogRunEvent(RunEvent{
		RunID:    "run-secret",
		Sequence: 1,
		Type:     "test",
		Metadata: map[string]any{
			"api_key": "sk-abcdefghijklmnopqrstuvwxyz0123456789",
		},
	})
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "run-run-redact.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Error("Expected API key to be redacted in run event")
	}
}

// ---------------------------------------------------------------------------
// LogError helper with nil metadata and nil error
// ---------------------------------------------------------------------------

func TestLogError_NilMetadata_NilError(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	GlobalLogger.SetSessionID("err-test")

	LogError(CatSystem, "test_err", "error message", nil, nil)

	GlobalLogger.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_err-test_audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "error message") {
		t.Errorf("Expected error message in log, got: %s", string(data))
	}
}

func TestLogError_WithMetadata_WithError(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	GlobalLogger.SetSessionID("err-test2")

	LogError(CatSystem, "test_err2", "something failed", fmt.Errorf("bad error"), map[string]any{"key": "val"})

	GlobalLogger.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_err-test2_audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "bad error") {
		t.Errorf("Expected error string in log, got: %s", s)
	}
	if !strings.Contains(s, "ERROR") {
		t.Errorf("Expected ERROR level in log, got: %s", s)
	}
}

// ---------------------------------------------------------------------------
// LogWrite with custom timestamp
// ---------------------------------------------------------------------------

func TestLogWrite_CustomTimestamp(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("ts-test")

	event := AuditEvent{
		Timestamp: "2024-01-01T00:00:00Z",
		Level:     "INFO",
		Category:  "system",
		Message:   "custom timestamp",
	}
	lm.LogWrite(event)
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_ts-test_audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "2024-01-01T00:00:00Z") {
		t.Errorf("Expected custom timestamp, got: %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// LogWrite plain text formatting
// ---------------------------------------------------------------------------

func TestLogWrite_PlainTextFormatting(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("plain-test")

	event := AuditEvent{
		Level:      "WARN",
		Category:   "security_gate",
		Event:      "test_event",
		Message:    "test message",
		DurationMS: 42,
		Metadata:   map[string]any{"key": "value"},
	}
	lm.LogWrite(event)
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_plain-test_audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "[WARN]") {
		t.Errorf("Missing [WARN] in plain log: %s", s)
	}
	if !strings.Contains(s, "[security_gate]") {
		t.Errorf("Missing [security_gate] in plain log: %s", s)
	}
	if !strings.Contains(s, "[test_event]") {
		t.Errorf("Missing [test_event] in plain log: %s", s)
	}
	if !strings.Contains(s, "duration=42ms") {
		t.Errorf("Missing duration=42ms in plain log: %s", s)
	}
	if !strings.Contains(s, "metadata=") {
		t.Errorf("Missing metadata= in plain log: %s", s)
	}
}

// ---------------------------------------------------------------------------
// ReadTraceTail with malformed JSON lines
// ---------------------------------------------------------------------------

func TestReadTraceTail_MalformedLines(t *testing.T) {
	tempDir := t.TempDir()
	origLogger := GlobalLogger
	GlobalLogger = &LoggerManager{logsDir: tempDir}
	defer func() { GlobalLogger = origLogger }()

	// Write a mix of valid and invalid JSON lines
	tracePath := filepath.Join(tempDir, "trace-mixed-sess.jsonl")
	content := `{"tool":"valid_tool","session_id":"mixed-sess","result_status":"ok","duration_ms":10}
not-json-at-all
{"tool":"another_valid","session_id":"mixed-sess","result_status":"ok","duration_ms":20}
`
	if err := os.WriteFile(tracePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	traces, err := ReadTraceTail("mixed-sess", 10)
	if err != nil {
		t.Fatalf("ReadTraceTail failed: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("Expected 2 valid traces (malformed line skipped), got %d", len(traces))
	}
	if traces[0].Tool != "valid_tool" {
		t.Errorf("First trace tool = %q, want 'valid_tool'", traces[0].Tool)
	}
	if traces[1].Tool != "another_valid" {
		t.Errorf("Second trace tool = %q, want 'another_valid'", traces[1].Tool)
	}
}

// ---------------------------------------------------------------------------
// cleanupOldTraceFiles with nonexistent directory
// ---------------------------------------------------------------------------

func TestCleanupOldTraceFiles_NonexistentDir(t *testing.T) {
	// Should not panic on nonexistent directory
	cleanupOldTraceFiles("/nonexistent/dir/that/does/not/exist")
}

// ---------------------------------------------------------------------------
// LogRunEvent with custom timestamp and schema version
// ---------------------------------------------------------------------------

func TestLogRunEvent_CustomTimestampAndSchema(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("run-custom")

	lm.LogRunEvent(RunEvent{
		SchemaVersion: 2,
		Timestamp:     "2024-06-01T12:00:00Z",
		RunID:         "run-custom-ts",
		Sequence:      1,
		Type:          "test",
	})
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "run-run-custom.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var event RunEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	// Schema version should NOT be overridden since it was set
	if event.SchemaVersion != 2 {
		t.Errorf("Expected schema_version=2, got %d", event.SchemaVersion)
	}
	// Timestamp should NOT be overridden since it was set
	if event.Timestamp != "2024-06-01T12:00:00Z" {
		t.Errorf("Expected custom timestamp, got %q", event.Timestamp)
	}
}

// ---------------------------------------------------------------------------
// LogWrite with no event and no metadata (plain text edge cases)
// ---------------------------------------------------------------------------

func TestLogWrite_PlainText_NoEventNoMeta(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("minimal")

	event := AuditEvent{
		Level:    "INFO",
		Category: "system",
		Message:  "minimal event",
	}
	lm.LogWrite(event)
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_minimal_audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	// Should contain [INFO] and [system] and the message
	if !strings.Contains(s, "[INFO]") {
		t.Errorf("Missing [INFO]: %s", s)
	}
	if !strings.Contains(s, "[system]") {
		t.Errorf("Missing [system]: %s", s)
	}
	if !strings.Contains(s, "minimal event") {
		t.Errorf("Missing message: %s", s)
	}
	// Should NOT contain metadata= since Metadata is nil
	if strings.Contains(s, "metadata=") {
		t.Errorf("Nil metadata should not produce metadata string: %s", s)
	}
	// Should NOT contain duration= since DurationMS is 0
	if strings.Contains(s, "duration=") {
		t.Errorf("Zero duration should not produce duration string: %s", s)
	}
	// No event string should appear after [system]
	if strings.Contains(s, "[system] [") {
		t.Errorf("Empty event should not produce extra brackets: %s", s)
	}
}

// ---------------------------------------------------------------------------
// LogWrite with sessionID already set on event
// ---------------------------------------------------------------------------

func TestLogWrite_EventWithSessionID(t *testing.T) {
	tempDir := t.TempDir()
	lm := &LoggerManager{logsDir: tempDir}
	lm.SetSessionID("logger-sess")

	event := AuditEvent{
		Level:     "INFO",
		SessionID: "custom-sess-id",
		Message:   "event with own session",
	}
	lm.LogWrite(event)
	lm.SetSessionID("")

	data, err := os.ReadFile(filepath.Join(tempDir, "session_logger-sess_audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The session ID in the event should be preserved (it was set)
	var parsed AuditEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.SessionID != "custom-sess-id" {
		t.Errorf("Expected session_id='custom-sess-id', got %q", parsed.SessionID)
	}
}
