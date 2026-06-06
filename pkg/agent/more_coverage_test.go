package agent

import "testing"

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		pattern string
		val     string
		want    bool
	}{
		{"*", "anything", true},
		{"", "anything", true},
		{"file_read", "file_read", true},
		{"file_read", "FILE_READ", true},
		{"file", "file_read", true},
		{"file_*", "file_read", true},
		{"file_*", "file_write", true},
		{"file_*", "shell_run", false},
		{"*.go", "main.go", true},
		{"*.go", "test.txt", false},
	}
	for _, tt := range tests {
		got := matchesPattern(tt.pattern, tt.val)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.val, got, tt.want)
		}
	}
}

func TestPermissionManager_SetMode(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	if err := pm.SetMode(ModeAuto); err != nil {
		t.Errorf("SetMode(auto) error: %v", err)
	}
	if pm.GetMode() != ModeAuto {
		t.Errorf("GetMode() = %v, want auto", pm.GetMode())
	}

	if err := pm.SetMode(PermissionMode("invalid")); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestPermissionManager_AddRule(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)
	before := len(pm.GetRules())
	pm.AddRule(PermissionRule{Tool: "test_tool_12345", Path: "*", Behavior: "allow"})
	rules := pm.GetRules()
	if len(rules) != before+1 {
		t.Fatalf("expected %d rules, got %d", before+1, len(rules))
	}
	found := false
	for _, r := range rules {
		if r.Tool == "test_tool_12345" {
			found = true
			break
		}
	}
	if !found {
		t.Error("added rule not found")
	}
}

func TestPermissionManager_ConsecutiveDenials(t *testing.T) {
	pm := NewPermissionManager(ModeDefault)

	if pm.ConsecutiveDenials() != 0 {
		t.Error("expected 0 denials initially")
	}

	pm.NoteDenial()
	if pm.ConsecutiveDenials() != 1 {
		t.Errorf("expected 1 denial, got %d", pm.ConsecutiveDenials())
	}
	pm.NoteDenial()
	if pm.ConsecutiveDenials() != 2 {
		t.Errorf("expected 2 denials, got %d", pm.ConsecutiveDenials())
	}

	pm.NoteApproval()
	if pm.ConsecutiveDenials() != 0 {
		t.Error("approval should reset denials")
	}

	pm.NoteDenial()
	pm.ResetConsecutiveDenials()
	if pm.ConsecutiveDenials() != 0 {
		t.Error("ResetConsecutiveDenials should reset count")
	}
}
