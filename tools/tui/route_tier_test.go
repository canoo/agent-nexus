package main

import "testing"

func TestClassifyPromptConfidenceAndTier(t *testing.T) {
	cases := []struct {
		prompt, destination, tier string
		confidence                float64
		low                       bool
	}{
		{"write a conventional commit message", "local", "cheap", 0.95, false},
		{"refactor this parser", "local", "main", 0.85, false},
		{"design the authentication architecture", "agy", "advisor", 0.90, false},
		{"help me improve this application", "agy", "advisor", 0.50, true},
	}
	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			got := classifyPrompt(tc.prompt)
			if got.Destination != tc.destination || got.Tier != tc.tier || got.Confidence != tc.confidence {
				t.Fatalf("got %#v, want destination=%s tier=%s confidence=%v", got, tc.destination, tc.tier, tc.confidence)
			}
			if isLowConfidence(got) != tc.low {
				t.Fatalf("isLowConfidence(%#v) = %v, want %v", got, isLowConfidence(got), tc.low)
			}
		})
	}
}
