package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// TestFormatHoverContents — table-driven tests for formatHoverContents
// ---------------------------------------------------------------------------

func TestFormatHoverContents(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty raw returns no hover info",
			raw:  "",
			want: "No hover information available.",
		},
		{
			name: "plain string JSON",
			raw:  `"hello world"`,
			want: "hello world",
		},
		{
			name: "MarkupContent object",
			raw:  `{"kind":"markdown","value":"**bold** text"}`,
			want: "**bold** text",
		},
		{
			name: "MarkupContent with empty value falls through to raw",
			raw:  `{"kind":"markdown","value":""}`,
			want: `{"kind":"markdown","value":""}`,
		},
		{
			name: "array of string MarkedStrings",
			raw:  `["line one","line two"]`,
			want: "line one\n\nline two",
		},
		{
			name: "array of {language,value} objects",
			raw:  `[{"language":"go","value":"func main()"},{"language":"python","value":"def main():"}]`,
			want: "func main()\n\ndef main():",
		},
		{
			name: "mixed array with string and object",
			raw:  `["type is string",{"language":"go","value":"func Foo()"}]`,
			want: "type is string\n\nfunc Foo()",
		},
		{
			name: "unparseable raw returns trimmed raw",
			raw:  `{not valid json`,
			want: "{not valid json",
		},
		{
			name: "array with empty items returns raw",
			raw:  `[]`,
			want: "[]",
		},
		{
			name: "single object with language and value",
			raw:  `{"language":"typescript","value":"const x = 1"}`,
			want: "const x = 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "" {
				raw = json.RawMessage(tt.raw)
			}
			got := formatHoverContents(raw)
			if got != tt.want {
				t.Errorf("formatHoverContents(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestSeverityToString — table-driven tests for severityToString
// ---------------------------------------------------------------------------

func TestSeverityToString(t *testing.T) {
	tests := []struct {
		severity int
		want     string
	}{
		{1, "error"},
		{2, "warning"},
		{3, "info"},
		{4, "hint"},
		{0, "info"},   // default
		{99, "info"},  // default
		{-1, "info"},  // default
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("severity_%d", tt.severity), func(t *testing.T) {
			got := severityToString(tt.severity)
			if got != tt.want {
				t.Errorf("severityToString(%d) = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}
