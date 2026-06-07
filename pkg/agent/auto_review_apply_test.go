package agent

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Direct coverage for the 10 security check functions in auto_review_apply.go.
// These are called indirectly via heuristicReview but the cover tool attributes
// coverage to the caller, not the individual check functions.
// ---------------------------------------------------------------------------

// --- checkHeredoc ---

func TestCheckHeredoc_Direct(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		safe   bool
		reason string
	}{
		{"safe_cat", "cat file.txt", true, ""},
		{"safe_echo", "echo hello world", true, ""},
		{"heredoc_double_dash", "cat <<-DELIM", false, "heredoc abuse detected"},
		{"heredoc_double", "cat <<DELIM", false, "heredoc abuse detected"},
		{"here_string", "cat <<< data", false, "heredoc abuse detected"},
		{"empty_cmd", "", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkHeredoc(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkHeredoc(%q) safe=%v, want %v", tt.cmd, safe, tt.safe)
			}
			if !tt.safe && reason == "" {
				t.Errorf("checkHeredoc(%q) unsafe but reason is empty", tt.cmd)
			}
		})
	}
}

// --- checkEnvExpansion ---

func TestCheckEnvExpansion_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		// Safe: env var without write context
		{"safe_echo_var", "echo $HOME", true},
		{"safe_no_var", "ls -la", true},
		{"safe_no_var_with_redirect", "echo hello > out.txt", true},
		// Unsafe: env var with write context
		{"unsafe_redirect_var", "echo $HOME > out.txt", false},
		{"unsafe_append_var", "echo ${PATH} >> log.txt", false},
		{"unsafe_tee_var", "echo $USER | tee output.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkEnvExpansion(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkEnvExpansion(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkProcessSubstitution ---

func TestCheckProcessSubstitution_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_diff", "diff a.txt b.txt", true},
		{"safe_cat", "cat file", true},
		{"unsafe_input_sub", "diff <(sort a.txt) <(sort b.txt)", false},
		{"unsafe_output_sub", "tee >(gzip > out.gz)", false},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkProcessSubstitution(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkProcessSubstitution(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkNamedPipe ---

func TestCheckNamedPipe_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_ls", "ls -la", true},
		{"unsafe_mkfifo", "mkfifo /tmp/pipe", false},
		{"unsafe_mknod", "mknod /tmp/pipe p", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkNamedPipe(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkNamedPipe(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkTTVEscape ---

func TestCheckTTVEscape_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_printf", `printf "hello world"`, true},
		{"unsafe_x1b", `printf "\x1b[2J"`, false},
		{"unsafe_033", `printf "\033[2J"`, false},
		{"unsafe_e_escape", `printf "\e[0m"`, false},
		{"unsafe_X1B_upper", `printf "\x1B"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkTTVEscape(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkTTVEscape(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkFileDescriptor ---

func TestCheckFileDescriptor_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_echo", "echo hello", true},
		{"unsafe_exec_fd", "exec 3>/tmp/out.txt", false},
		{"unsafe_redirect_fd", "command >&2", false},
		{"unsafe_read_fd", "command <&3", false},
		{"safe_exec_command", "exec ls", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkFileDescriptor(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkFileDescriptor(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkUnsafeSource ---

func TestCheckUnsafeSource_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_ls", "ls -la", true},
		{"safe_source_relative", "source ./script.sh", true},
		{"unsafe_source_abs", "source /etc/malicious.sh", false},
		{"unsafe_dot_abs", ". /tmp/evil.sh", false},
		{"unsafe_source_root", "source /root/.bashrc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkUnsafeSource(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkUnsafeSource(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkEncodingAttack ---

func TestCheckEncodingAttack_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_echo", `echo "hello"`, true},
		{"unsafe_hex", `echo "\x41"`, false},
		{"unsafe_unicode_short", "printf \"\\u0041\"", false},
		{"unsafe_unicode_long", `echo "\U00000041"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkEncodingAttack(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkEncodingAttack(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkProxyInjection ---

func TestCheckProxyInjection_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_git_clone", "git clone https://github.com/repo", true},
		{"safe_ssh", "ssh user@host", true},
		{"unsafe_proxy_command", "ssh -o ProxyCommand=evil user@host", false},
		{"unsafe_git_config", "git -c core.sshCommand=evil clone url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkProxyInjection(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkProxyInjection(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}

// --- checkUnsafeFindPipe ---

func TestCheckUnsafeFindPipe_Direct(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"safe_find", "find . -name '*.go'", true},
		{"unsafe_find_rm", "find . -name '*.log' | while read f; do rm \"$f\"; done", false},
		{"unsafe_find_mv", "find /tmp -type f | while read x; do mv \"$x\" /evil; done", false},
		{"safe_find_pipe_grep", "find . -name '*.go' | grep -v test", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, reason := checkUnsafeFindPipe(tt.cmd)
			if safe != tt.safe {
				t.Errorf("checkUnsafeFindPipe(%q) safe=%v, want %v, reason=%q", tt.cmd, safe, tt.safe, reason)
			}
		})
	}
}
