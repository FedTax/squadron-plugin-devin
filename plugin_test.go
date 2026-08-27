package main

import (
	"strings"
	"testing"
)

func TestBuildDevelopPromptRawOmitsWorkflow(t *testing.T) {
	prompt := buildDevelopPrompt("Investigate DEV-8126, read-only.", "devin/existing", "Do not open a PR.", true)

	for _, injected := range []string{"Create a new branch", "Open a pull request", "Add or update tests"} {
		if strings.Contains(prompt, injected) {
			t.Errorf("raw prompt should not inject %q:\n%s", injected, prompt)
		}
	}
	for _, want := range []string{"Investigate DEV-8126, read-only.", "devin/existing", "Do not open a PR."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("raw prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildDevelopPromptDefaultKeepsWorkflow(t *testing.T) {
	prompt := buildDevelopPrompt("Add pagination", "feature/pages", "Follow the /orders pattern.", false)

	for _, want := range []string{"Create a new branch", "feature/pages", "Open a pull request", "Follow the /orders pattern."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("default prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestLastDevinMessage(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantOK  bool
	}{
		{
			name:    "wrapped messages, devin last",
			payload: `{"messages":[{"type":"user_message","message":"go"},{"type":"devin_message","message":"Done: PR #201"}]}`,
			want:    "Done: PR #201",
			wantOK:  true,
		},
		{
			name:    "bare array, trailing user message skipped",
			payload: `[{"type":"devin_message","message":"verdict: DEFECT_PROVEN"},{"type":"user_message","message":"thanks"}]`,
			want:    "verdict: DEFECT_PROVEN",
			wantOK:  true,
		},
		{
			name:    "alternate field names",
			payload: `[{"role":"assistant","text":"WAI_CONFIRMED"}]`,
			want:    "WAI_CONFIRMED",
			wantOK:  true,
		},
		{
			name:    "no devin message",
			payload: `{"messages":[{"type":"user_message","message":"go"}]}`,
			want:    "",
			wantOK:  true,
		},
		{
			name:    "unrecognised shape falls back to raw",
			payload: `{"conversation":{"turns":[]}}`,
			want:    "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lastDevinMessage(tc.payload)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
		})
	}
}
