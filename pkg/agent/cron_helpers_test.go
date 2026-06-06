package agent

import (
	"os"
	"testing"
)

// --- hashString tests ---

func TestHashString_Empty(t *testing.T) {
	result := hashString("")
	if result != 0 {
		t.Errorf("expected hash of empty string to be 0, got %d", result)
	}
}

func TestHashString_SingleChar(t *testing.T) {
	// h = 31*0 + 'a' = 97
	result := hashString("a")
	if result != 97 {
		t.Errorf("expected hash('a')=97, got %d", result)
	}
}

func TestHashString_Deterministic(t *testing.T) {
	s := "hello world"
	h1 := hashString(s)
	h2 := hashString(s)
	if h1 != h2 {
		t.Errorf("hashString should be deterministic, got %d and %d for %q", h1, h2, s)
	}
}

func TestHashString_DifferentStrings(t *testing.T) {
	h1 := hashString("abc")
	h2 := hashString("abd")
	if h1 == h2 {
		t.Error("different strings should (very likely) produce different hashes")
	}
}

func TestHashString_LongString(t *testing.T) {
	// Should not panic on long strings
	longStr := string(make([]byte, 10000))
	for i := range longStr {
		longStr = longStr[:i] + "x" + longStr[i+1:]
	}
	_ = hashString(longStr)
}

// --- isPIDAlive tests ---

func TestIsPIDAlive_CurrentProcess(t *testing.T) {
	// Current process PID should be alive
	pid := os.Getpid()
	if !isPIDAlive(pid) {
		t.Errorf("current process PID %d should be alive", pid)
	}
}

func TestIsPIDAlive_InitProcess(t *testing.T) {
	// PID 1 (launchd on macOS) should be alive
	if !isPIDAlive(1) {
		t.Error("PID 1 should be alive")
	}
}

func TestIsPIDAlive_NonexistentPID(t *testing.T) {
	// Use a very high PID that is extremely unlikely to exist
	// On macOS, max PID is 99999 by default
	// Use a PID in the middle range that is very likely not allocated
	// We can't guarantee this, but PID 300000 is well above typical max
	alive := isPIDAlive(300000)
	if alive {
		// It's possible a process has this PID, but very unlikely
		t.Log("PID 300000 is alive (unusual but possible)")
	}
}

func TestIsPIDAlive_NegativePID(t *testing.T) {
	// Negative PID: just verify it doesn't panic.
	// On macOS, FindProcess(-1) succeeds but Signal returns ESRCH or similar.
	_ = isPIDAlive(-1)
}

func TestIsPIDAlive_ZeroPID(t *testing.T) {
	// PID 0 (kernel/swapper) - on macOS sending signal(0) to PID 0
	// typically returns permission denied, which means the process exists
	// but we can't signal it. The function returns true in this case
	// because err is neither ESRCH nor ErrProcessDone.
	_ = isPIDAlive(0)
	// No assertion needed - just verify it doesn't panic
}

// --- fieldMatches additional tests ---

func TestFieldMatches_StepWithWildcard(t *testing.T) {
	// */5 with value 10, range 0-59: (10-0)%5 == 0 => true
	if !fieldMatches("*/5", 10, 0, 59) {
		t.Error("*/5 should match value 10")
	}
	// */5 with value 7, range 0-59: (7-0)%5 != 0 => false
	if fieldMatches("*/5", 7, 0, 59) {
		t.Error("*/5 should not match value 7")
	}
}

func TestFieldMatches_RangeWithStep(t *testing.T) {
	// 10-20/5 with value 15: 15 >= 10 && 15 <= 20 && (15-10)%5 == 0 => true
	if !fieldMatches("10-20/5", 15, 0, 59) {
		t.Error("10-20/5 should match value 15")
	}
	// 10-20/5 with value 12: 12 >= 10 && 12 <= 20 && (12-10)%5 != 0 => false
	if fieldMatches("10-20/5", 12, 0, 59) {
		t.Error("10-20/5 should not match value 12")
	}
}

func TestFieldMatches_ExactValue(t *testing.T) {
	if !fieldMatches("30", 30, 0, 59) {
		t.Error("30 should match value 30")
	}
	if fieldMatches("30", 31, 0, 59) {
		t.Error("30 should not match value 31")
	}
}

func TestFieldMatches_SundayDOW(t *testing.T) {
	// Sunday can be represented as 7 in cron, but value is 0
	// lo=0, hi=6 is the DOW field
	if !fieldMatches("7", 0, 0, 6) {
		t.Error("7 should match Sunday (0) in DOW field")
	}
	if !fieldMatches("0", 0, 0, 6) {
		t.Error("0 should also match Sunday (0) in DOW field")
	}
}

func TestFieldMatches_List(t *testing.T) {
	if !fieldMatches("1,15,30", 15, 0, 59) {
		t.Error("1,15,30 should match value 15")
	}
	if fieldMatches("1,15,30", 10, 0, 59) {
		t.Error("1,15,30 should not match value 10")
	}
}

func TestFieldMatches_Range(t *testing.T) {
	if !fieldMatches("10-20", 15, 0, 59) {
		t.Error("10-20 should match value 15")
	}
	if fieldMatches("10-20", 25, 0, 59) {
		t.Error("10-20 should not match value 25")
	}
}

func TestFieldMatches_InvalidStepZero(t *testing.T) {
	// Step of 0 should default to 1
	if !fieldMatches("*/0", 5, 0, 59) {
		t.Error("*/0 with step defaulting to 1 should match value 5")
	}
}
