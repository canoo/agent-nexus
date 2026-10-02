package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
)

// The adapter accepts at most one MiB from Tokscale. This is ample for a
// model-aggregate report while preventing an unexpectedly large report from
// becoming TUI data.
const maxTokscaleOutputBytes = 1 << 20

// Version output is intentionally much smaller than a report. Only a parsed
// semantic version is kept, never the command's raw text.
const maxTokscaleVersionOutputBytes = 4096

var tokscaleVersionPattern = regexp.MustCompile(`(?i)\bv?(\d+\.\d+\.\d+(?:[-+][0-9a-z.-]+)*)`)

var (
	// ErrTokscaleUnavailable means the Tokscale executable was not found.
	ErrTokscaleUnavailable = errors.New("tokscale unavailable")
	// ErrTokscaleCommand means Tokscale did not complete successfully.
	ErrTokscaleCommand = errors.New("tokscale command failed")
	// ErrTokscaleMalformedOutput means Tokscale did not return a valid model report.
	ErrTokscaleMalformedOutput = errors.New("tokscale returned malformed output")
)

// commandRunner isolates process execution so adapter tests do not require
// Tokscale to be installed.
type commandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// TokscaleAdapter loads local usage aggregates from an already installed
// Tokscale CLI. It does not install, configure, or otherwise invoke Tokscale.
type TokscaleAdapter struct {
	Runner commandRunner
	Binary string // defaults to "tokscale"
}

// Version reports Tokscale's installed semantic version using its fixed,
// non-interactive version command. It never returns unparsed command output.
func (a TokscaleAdapter) Version(ctx context.Context) (string, error) {
	runner := a.runner()
	output, err := runner.Output(ctx, a.binary(), "--version")
	if err != nil {
		return "", classifyTokscaleCommandError(err)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("%w: command did not complete", ErrTokscaleCommand)
	}
	if len(output) > maxTokscaleVersionOutputBytes {
		return "", fmt.Errorf("%w: version exceeds size limit", ErrTokscaleMalformedOutput)
	}

	match := tokscaleVersionPattern.FindSubmatch(output)
	if len(match) != 2 || len(match[1]) > 64 {
		return "", fmt.Errorf("%w: invalid version", ErrTokscaleMalformedOutput)
	}
	return "v" + string(match[1]), nil
}

// TokscaleReport is the allowlisted portion of Tokscale's model report.
type TokscaleReport struct {
	GroupBy string
	Entries []TokscaleUsage
}

// TokscaleUsage contains only the aggregate fields used by NEXUS.
type TokscaleUsage struct {
	Client, Provider, Model                            string
	InputTokens, OutputTokens                          int64
	CacheReadTokens, CacheWriteTokens, ReasoningTokens int64
	MessageCount                                       int64
	CostUSD                                            float64
	SessionID                                          string
}

// Load obtains Tokscale's default model report. Its fixed arguments are kept
// here intentionally: callers cannot supply an interactive command or filters.
func (a TokscaleAdapter) Load(ctx context.Context) (TokscaleReport, error) {
	output, err := a.runner().Output(ctx, a.binary(), "models", "--json")
	if err != nil {
		return TokscaleReport{}, classifyTokscaleCommandError(err)
	}
	if ctx.Err() != nil {
		return TokscaleReport{}, fmt.Errorf("%w: command did not complete", ErrTokscaleCommand)
	}
	if len(output) > maxTokscaleOutputBytes {
		return TokscaleReport{}, fmt.Errorf("%w: report exceeds size limit", ErrTokscaleMalformedOutput)
	}

	report, err := parseTokscaleReport(output)
	if err != nil {
		// Parser errors intentionally do not identify values from the response.
		return TokscaleReport{}, fmt.Errorf("%w: invalid model report", ErrTokscaleMalformedOutput)
	}
	return report, nil
}

func (a TokscaleAdapter) runner() commandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return execCommandRunner{}
}

func (a TokscaleAdapter) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "tokscale"
}

func classifyTokscaleCommandError(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		// exec.ErrNotFound is a stable sentinel and contains no CLI output.
		return fmt.Errorf("%w: %w", ErrTokscaleUnavailable, exec.ErrNotFound)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrTokscaleCommand, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrTokscaleCommand, context.DeadlineExceeded)
	}
	// Do not wrap the runner error: a third-party CLI error could include
	// output that must never reach the TUI or any NEXUS diagnostics.
	return fmt.Errorf("%w: command did not complete", ErrTokscaleCommand)
}

func parseTokscaleReport(output []byte) (TokscaleReport, error) {
	var envelope map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return TokscaleReport{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return TokscaleReport{}, errors.New("multiple JSON values")
		}
		return TokscaleReport{}, err
	}

	groupBy, err := requiredString(envelope, "groupBy")
	if err != nil {
		return TokscaleReport{}, err
	}
	entriesRaw, ok := envelope["entries"]
	if !ok {
		return TokscaleReport{}, errors.New("entries missing")
	}
	if trimmed := bytes.TrimSpace(entriesRaw); len(trimmed) == 0 || trimmed[0] != '[' {
		return TokscaleReport{}, errors.New("entries is not an array")
	}

	var rawEntries []json.RawMessage
	entriesDecoder := json.NewDecoder(bytes.NewReader(entriesRaw))
	entriesDecoder.UseNumber()
	if err := entriesDecoder.Decode(&rawEntries); err != nil {
		return TokscaleReport{}, err
	}

	report := TokscaleReport{GroupBy: groupBy, Entries: make([]TokscaleUsage, 0, len(rawEntries))}
	for _, rawEntry := range rawEntries {
		usage, err := parseTokscaleUsage(rawEntry)
		if err != nil {
			return TokscaleReport{}, err
		}
		report.Entries = append(report.Entries, usage)
	}
	return report, nil
}

func parseTokscaleUsage(rawEntry json.RawMessage) (TokscaleUsage, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(rawEntry))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return TokscaleUsage{}, err
	}

	client, err := requiredString(fields, "client")
	if err != nil {
		return TokscaleUsage{}, err
	}
	provider, err := requiredString(fields, "provider")
	if err != nil {
		return TokscaleUsage{}, err
	}
	model, err := requiredString(fields, "model")
	if err != nil {
		return TokscaleUsage{}, err
	}
	inputTokens, err := requiredCount(fields, "input")
	if err != nil {
		return TokscaleUsage{}, err
	}
	outputTokens, err := requiredCount(fields, "output")
	if err != nil {
		return TokscaleUsage{}, err
	}
	cacheReadTokens, err := requiredCount(fields, "cacheRead")
	if err != nil {
		return TokscaleUsage{}, err
	}
	cacheWriteTokens, err := requiredCount(fields, "cacheWrite")
	if err != nil {
		return TokscaleUsage{}, err
	}
	reasoningTokens, err := requiredCount(fields, "reasoning")
	if err != nil {
		return TokscaleUsage{}, err
	}
	messageCount, err := requiredCount(fields, "messageCount")
	if err != nil {
		return TokscaleUsage{}, err
	}
	costUSD, err := requiredCost(fields, "cost")
	if err != nil {
		return TokscaleUsage{}, err
	}

	sessionID, err := optionalString(fields, "sessionId")
	if err != nil {
		return TokscaleUsage{}, err
	}

	return TokscaleUsage{
		Client:           client,
		Provider:         provider,
		Model:            model,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		ReasoningTokens:  reasoningTokens,
		MessageCount:     messageCount,
		CostUSD:          costUSD,
		SessionID:        sessionID,
	}, nil
}

func requiredString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", errors.New("required string missing")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", errors.New("expected required string")
	}
	return stringValue, nil
}

func optionalString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", errors.New("expected optional string")
	}
	return stringValue, nil
}

func requiredCount(fields map[string]json.RawMessage, name string) (int64, error) {
	number, err := requiredJSONNumber(fields, name)
	if err != nil {
		return 0, err
	}
	value, err := number.Int64()
	if err != nil || value < 0 {
		return 0, errors.New("invalid non-negative count")
	}
	return value, nil
}

func requiredCost(fields map[string]json.RawMessage, name string) (float64, error) {
	number, err := requiredJSONNumber(fields, name)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid non-negative cost")
	}
	return value, nil
}

func requiredJSONNumber(fields map[string]json.RawMessage, name string) (json.Number, error) {
	raw, ok := fields[name]
	if !ok {
		return "", errors.New("required number missing")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	number, ok := value.(json.Number)
	if !ok {
		return "", errors.New("expected JSON number")
	}
	return number, nil
}
