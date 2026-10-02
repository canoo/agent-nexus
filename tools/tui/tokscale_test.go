package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type fakeTokscaleRunner struct {
	output []byte
	err    error
	ctx    context.Context
	name   string
	args   []string
}

func (r *fakeTokscaleRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.ctx = ctx
	r.name = name
	r.args = append([]string(nil), args...)
	return r.output, r.err
}

const validTokscaleReport = `{
  "groupBy": "model",
  "upstreamMetadata": "must not be retained",
  "entries": [
    {
      "client": "codex",
      "provider": "openai",
      "model": "gpt-5",
      "input": 9007199254740991,
      "output": 42,
      "cacheRead": 7,
      "cacheWrite": 8,
      "reasoning": 9,
      "messageCount": 10,
      "cost": 12.34,
      "sessionId": "session-1",
      "upstreamSecret": "must not be retained"
    },
    {
      "client": "claude",
      "provider": "anthropic",
      "model": "claude-sonnet",
      "input": 1,
      "output": 2,
      "cacheRead": 3,
      "cacheWrite": 4,
      "reasoning": 5,
      "messageCount": 6,
      "cost": 0
    }
  ]
}`

func TestTokscaleAdapterLoadMapsAllowlistedUsage(t *testing.T) {
	ctx := context.WithValue(context.Background(), "test-context", "present")
	runner := &fakeTokscaleRunner{output: []byte(validTokscaleReport)}
	report, err := (TokscaleAdapter{Runner: runner}).Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if runner.ctx != ctx || runner.name != "tokscale" || strings.Join(runner.args, " ") != "models --json" {
		t.Fatalf("unexpected command: context=%v name=%q args=%q", runner.ctx == ctx, runner.name, runner.args)
	}
	if report.GroupBy != "model" || len(report.Entries) != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	first := report.Entries[0]
	if first.Client != "codex" || first.Provider != "openai" || first.Model != "gpt-5" ||
		first.InputTokens != 9007199254740991 || first.OutputTokens != 42 ||
		first.CacheReadTokens != 7 || first.CacheWriteTokens != 8 || first.ReasoningTokens != 9 ||
		first.MessageCount != 10 || first.CostUSD != 12.34 || first.SessionID != "session-1" {
		t.Errorf("first entry = %#v", first)
	}
	second := report.Entries[1]
	if second.SessionID != "" || second.InputTokens != 1 || second.CostUSD != 0 {
		t.Errorf("optional session ID or fields mapped incorrectly: %#v", second)
	}
}

func TestTokscaleAdapterLoadUsesConfiguredBinary(t *testing.T) {
	runner := &fakeTokscaleRunner{output: []byte(`{"groupBy":"model","entries":[]}`)}
	_, err := (TokscaleAdapter{Runner: runner, Binary: "custom-tokscale"}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if runner.name != "custom-tokscale" {
		t.Errorf("name = %q, want configured binary", runner.name)
	}
}

func TestTokscaleAdapterVersionUsesFixedCommandAndParsesOnlySemanticVersion(t *testing.T) {
	runner := &fakeTokscaleRunner{output: []byte("tokscale v1.2.3-beta.1+build.4 private-marker")}
	version, err := (TokscaleAdapter{Runner: runner, Binary: "custom-tokscale"}).Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version != "v1.2.3-beta.1+build.4" {
		t.Fatalf("version = %q", version)
	}
	if runner.name != "custom-tokscale" || strings.Join(runner.args, " ") != "--version" {
		t.Fatalf("unexpected command: %q %q", runner.name, runner.args)
	}
}

func TestTokscaleAdapterVersionClassifiesUnsafeOutput(t *testing.T) {
	for _, tt := range []struct {
		name   string
		output []byte
		err    error
		want   error
	}{
		{name: "unavailable", err: exec.ErrNotFound, want: ErrTokscaleUnavailable},
		{name: "command failure", err: errors.New("private command error"), want: ErrTokscaleCommand},
		{name: "malformed", output: []byte("private-version-marker"), want: ErrTokscaleMalformedOutput},
		{name: "too large", output: make([]byte, maxTokscaleVersionOutputBytes+1), want: ErrTokscaleMalformedOutput},
	} {
		t.Run(tt.name, func(t *testing.T) {
			version, err := (TokscaleAdapter{Runner: &fakeTokscaleRunner{output: tt.output, err: tt.err}}).Version(context.Background())
			if !errors.Is(err, tt.want) || version != "" {
				t.Fatalf("Version() = %q, %v; want %v and no version", version, err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatalf("error leaked raw command output: %q", err)
			}
		})
	}
}

func TestTokscaleAdapterLoadClassifiesCommandFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "unavailable", err: exec.ErrNotFound, want: ErrTokscaleUnavailable},
		{name: "non-zero", err: errors.New("exit status 1"), want: ErrTokscaleCommand},
		{name: "cancelled", err: context.Canceled, want: ErrTokscaleCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := (TokscaleAdapter{Runner: &fakeTokscaleRunner{err: tt.err}}).Load(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.want)
			}
			if report.GroupBy != "" || len(report.Entries) != 0 {
				t.Errorf("failure returned partial report: %#v", report)
			}
		})
	}
}

func TestTokscaleAdapterLoadClassifiesTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := (TokscaleAdapter{Runner: &fakeTokscaleRunner{output: []byte(validTokscaleReport)}}).Load(ctx)
	if !errors.Is(err, ErrTokscaleCommand) {
		t.Fatalf("error = %v, want ErrTokscaleCommand", err)
	}
	if len(report.Entries) != 0 {
		t.Errorf("timeout returned partial report: %#v", report)
	}
}

func TestTokscaleAdapterLoadRejectsMalformedOutputWithoutLeakingIt(t *testing.T) {
	tooLarge := make([]byte, maxTokscaleOutputBytes+1)
	for _, tt := range []struct {
		name   string
		output string
	}{
		{name: "invalid JSON", output: `{"entries": ["private-fixture-marker"`},
		{name: "wrong envelope", output: `[]`},
		{name: "missing entries", output: `{"groupBy":"model"}`},
		{name: "null entries", output: `{"groupBy":"model","entries":null}`},
		{name: "invalid group", output: `{"groupBy":null,"entries":[]}`},
		{name: "invalid client", output: strings.Replace(validTokscaleReport, `"client": "codex"`, `"client": null`, 1)},
		{name: "negative count", output: strings.Replace(validTokscaleReport, `"input": 9007199254740991`, `"input": -1`, 1)},
		{name: "overflow count", output: strings.Replace(validTokscaleReport, `"input": 9007199254740991`, `"input": 9223372036854775808`, 1)},
		{name: "negative cost", output: strings.Replace(validTokscaleReport, `"cost": 12.34`, `"cost": -0.01`, 1)},
		{name: "overflow cost", output: strings.Replace(validTokscaleReport, `"cost": 12.34`, `"cost": 1e309`, 1)},
		{name: "invalid session ID", output: strings.Replace(validTokscaleReport, `"sessionId": "session-1"`, `"sessionId": null`, 1)},
		{name: "too large", output: string(tooLarge)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report, err := (TokscaleAdapter{Runner: &fakeTokscaleRunner{output: []byte(tt.output)}}).Load(context.Background())
			if !errors.Is(err, ErrTokscaleMalformedOutput) {
				t.Fatalf("error = %v, want ErrTokscaleMalformedOutput", err)
			}
			if report.GroupBy != "" || len(report.Entries) != 0 {
				t.Errorf("malformed output returned partial report: %#v", report)
			}
			if strings.Contains(err.Error(), "private-fixture-marker") {
				t.Errorf("error leaks output: %q", err)
			}
		})
	}
}
