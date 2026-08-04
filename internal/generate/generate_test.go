package generate

import (
	"strings"
	"testing"
)

func TestStripMarkdownCodeBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain json", `{"summary":"a","icon":"sun"}`, `{"summary":"a","icon":"sun"}`},
		{"json fence", "```json\n{\"summary\":\"a\"}\n```", `{"summary":"a"}`},
		{"bare fence", "```\n{\"summary\":\"a\"}\n```", `{"summary":"a"}`},
		{"leading whitespace", "  \n{\"summary\":\"a\"}  ", `{"summary":"a"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripMarkdownCodeBlock(tc.in); got != tc.want {
				t.Errorf("stripMarkdownCodeBlock(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildFinalPrompt(t *testing.T) {
	shots := []shot{
		{Input: `{"a":1}`, Output: `{"b":2}`},
	}
	got := buildFinalPrompt("do the thing", shots, `{"c":3}`)

	for _, want := range []string{"do the thing", "<examples>", "<example>", `{"a":1}`, `{"b":2}`, "</examples>", `input: {"c":3}`} {
		if !strings.Contains(got, want) {
			t.Errorf("buildFinalPrompt output missing %q\ngot: %s", want, got)
		}
	}
}
