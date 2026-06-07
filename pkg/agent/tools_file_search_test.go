package agent

import (
	"testing"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"star_go_matches_file", "*.go", "main.go", true},
		{"star_go_no_match_dir", "*.go", "dir/main.go", false},
		{"globstar_go_matches_dir", "**/*.go", "dir/main.go", true},
		{"globstar_go_deep", "**/*.go", "a/b/c/main.go", true},
		{"globstar_go_zero_segments", "**/*.go", "main.go", true},
		{"src_globstar_ts", "src/**/*.ts", "src/a/b/file.ts", true},
		{"src_globstar_ts_wrong_prefix", "src/**/*.ts", "lib/file.ts", false},
		{"star_any_file", "*", "anything.txt", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchGlob(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchGlobParts(t *testing.T) {
	tests := []struct {
		name    string
		pattern []string
		path    []string
		want    bool
	}{
		{"both_empty", []string{}, []string{}, true},
		{"pattern_longer", []string{"a", "b"}, []string{"a"}, false},
		{"globstar_zero_segments", []string{"**", "*.go"}, []string{"main.go"}, true},
		{"globstar_multiple_segments", []string{"**", "*.go"}, []string{"a", "b", "main.go"}, true},
		{"consecutive_globstars", []string{"**", "**", "*.go"}, []string{"a", "b", "main.go"}, true},
		{"question_mark_wildcard", []string{"?.go"}, []string{"a.go"}, true},
		{"question_mark_no_match", []string{"?.go"}, []string{"ab.go"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchGlobParts(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("matchGlobParts(%v, %v) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestSortFiles(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", []string{}, []string{}},
		{"single", []string{"a.go"}, []string{"a.go"}},
		{"unsorted", []string{"c.go", "a.go", "b.go"}, []string{"a.go", "b.go", "c.go"}},
		{"already_sorted", []string{"a.go", "b.go", "c.go"}, []string{"a.go", "b.go", "c.go"}},
		{"reverse_sorted", []string{"c.go", "b.go", "a.go"}, []string{"a.go", "b.go", "c.go"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortFiles(tc.in)
			if len(tc.in) != len(tc.want) {
				t.Fatalf("sortFiles() = %v, want %v (length mismatch)", tc.in, tc.want)
			}
			for i := range tc.in {
				if tc.in[i] != tc.want[i] {
					t.Errorf("sortFiles()[%d] = %q, want %q", i, tc.in[i], tc.want[i])
				}
			}
		})
	}
}
