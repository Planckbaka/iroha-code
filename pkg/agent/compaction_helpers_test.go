package agent

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestSortedKeys(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]bool
		want []string
	}{
		{"empty", map[string]bool{}, nil},
		{"single_key", map[string]bool{"a": true}, []string{"a"}},
		{"multiple_unsorted", map[string]bool{"c": true, "a": true, "b": true}, []string{"a", "b", "c"}},
		{"keys_with_slashes", map[string]bool{"z/a": true, "a/b": true, "m/n": true}, []string{"a/b", "m/n", "z/a"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedKeys(tc.m)
			if len(got) != len(tc.want) {
				t.Fatalf("sortedKeys() = %v, want %v (length mismatch)", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("sortedKeys()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestExtractStructuredSummary(t *testing.T) {
	tests := []struct {
		name        string
		rounds      []*genai.Content
		wantEmpty   bool
		wantContain []string
	}{
		{
			name:      "empty_rounds",
			rounds:    []*genai.Content{},
			wantEmpty: true,
		},
		{
			name: "tool_names_only",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "file_read"}},
						{FunctionCall: &genai.FunctionCall{Name: "search_grep"}},
					},
				},
			},
			wantContain: []string{"[SUMMARY]", "file_read", "search_grep", "Tools used:", "[/SUMMARY]"},
		},
		{
			name: "file_paths_in_text",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "I modified pkg/agent/tools.go and pkg/agent/runner.go"},
					},
				},
			},
			wantContain: []string{"[SUMMARY]", "Files:", "tools.go", "[/SUMMARY]"},
		},
		{
			name: "decision_phrases",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "Let's refactor the handler\nI'll fix the bug now\nWe should add tests"},
					},
				},
			},
			wantContain: []string{"[SUMMARY]", "Decisions:", "Let's refactor", "I'll fix", "We should add", "[/SUMMARY]"},
		},
		{
			name: "file_paths_from_args",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{
							FunctionCall: &genai.FunctionCall{
								Name: "file_read",
								Args: map[string]any{
									"path": "pkg/agent/config.go",
								},
							},
						},
					},
				},
			},
			wantContain: []string{"[SUMMARY]", "config.go", "[/SUMMARY]"},
		},
		{
			name: "combined_tools_files_decisions",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "file_write"}},
						{Text: "I will update main.go with the new logic"},
					},
				},
			},
			wantContain: []string{"[SUMMARY]", "Tools used:", "Files:", "Decisions:", "[/SUMMARY]"},
		},
		{
			name: "decision_cap_at_10",
			rounds: func() []*genai.Content {
				// Create 12 decision lines
				var lines []string
				for i := 0; i < 12; i++ {
					lines = append(lines, "I'll fix bug number %d")
				}
				text := strings.Join(lines, "\n")
				return []*genai.Content{
					{
						Role:  "model",
						Parts: []*genai.Part{{Text: text}},
					},
				}
			}(),
			wantContain: []string{"[SUMMARY]", "Decisions:", "[/SUMMARY]"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractStructuredSummary(tc.rounds)
			if tc.wantEmpty {
				if got != "" {
					t.Errorf("extractStructuredSummary() = %q, want empty string", got)
				}
				return
			}
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("extractStructuredSummary() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestSummarizeRounds_NoLLM(t *testing.T) {
	tests := []struct {
		name        string
		rounds      []*genai.Content
		wantContain []string
	}{
		{
			name:        "empty_rounds",
			rounds:      []*genai.Content{},
			wantContain: []string{"No previous conversation history"},
		},
		{
			name: "single_text_round",
			rounds: []*genai.Content{
				{
					Role:  "model",
					Parts: []*genai.Part{{Text: "Hello, I am helping you."}},
				},
			},
			wantContain: []string{"assistant", "Hello, I am helping you"},
		},
		{
			name: "model_role_maps_to_assistant",
			rounds: []*genai.Content{
				{
					Role:  "model",
					Parts: []*genai.Part{{Text: "response text"}},
				},
			},
			wantContain: []string{"assistant: response text"},
		},
		{
			name: "function_call_and_response",
			rounds: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "file_read"}},
					},
				},
				{
					Role: "user",
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{Name: "file_read"}},
					},
				},
			},
			wantContain: []string{"[Called tool file_read]", "tool file_read: [responded]"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Call with no LLM arguments to exercise the no-LLM fallback path
			got := summarizeRounds(tc.rounds)
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("summarizeRounds() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}
