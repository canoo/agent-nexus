package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type companionOutput struct {
	SchemaVersion int     `json:"schemaVersion"`
	Ok            bool    `json:"ok"`
	Action        *string `json:"action,omitempty"`
	RetentionDays *int64  `json:"retentionDays,omitempty"`
	Deleted       *int64  `json:"deleted,omitempty"`
	StoredSpans   *int64  `json:"storedSpans,omitempty"`
	Error         *string `json:"error,omitempty"`
}

type helperResponse struct {
	SchemaVersion *int    `json:"schemaVersion"`
	Ok            *bool   `json:"ok"`
	Action        *string `json:"action"`
	RetentionDays *int64  `json:"retentionDays"`
	Deleted       *int64  `json:"deleted"`
	StoredSpans   *int64  `json:"storedSpans"`
	Error         *string `json:"error"`
}

func emitError(out io.Writer, code string) {
	resp := companionOutput{
		SchemaVersion: 1,
		Ok:            false,
		Error:         &code,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(out, "%s\n", data)
}

func emitSuccess(out io.Writer, resp companionOutput) bool {
	resp.SchemaVersion = 1
	resp.Ok = true
	data, _ := json.Marshal(resp)
	_, err := fmt.Fprintf(out, "%s\n", data)
	return err == nil
}

func isDigitsOnly(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func runCompanionCLI(args []string, nexusDir string, out io.Writer) int {
	var expectedAction string
	var helperArgs []string
	var reqDays *int

	if len(args) == 2 && args[0] == "data" && args[1] == "--json" {
		expectedAction = "status"
		helperArgs = []string{"status"}
	} else if len(args) == 2 && args[0] == "prune" && args[1] == "--json" {
		expectedAction = "prune"
		helperArgs = []string{"prune"}
	} else if len(args) == 4 && args[0] == "retention" && args[1] == "--days" && args[3] == "--json" {
		daysStr := args[2]
		if !isDigitsOnly(daysStr) {
			emitError(out, "companion_command_invalid")
			return 2
		}
		daysVal, err := strconv.Atoi(daysStr)
		if err != nil || daysVal < 0 || daysVal > 365 {
			emitError(out, "companion_command_invalid")
			return 2
		}
		reqDays = &daysVal
		expectedAction = "retention"
		helperArgs = []string{"retention", "--days", daysStr}
	} else if len(args) == 3 && args[0] == "clear" && args[1] == "--confirm" && args[2] == "--json" {
		expectedAction = "clear"
		helperArgs = []string{"clear", "--confirm"}
	} else {
		emitError(out, "companion_command_invalid")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	scriptPath := filepath.Join(nexusDir, "tools", "mcp", "companion-data.mjs")
	cmd := exec.CommandContext(ctx, "node", append([]string{scriptPath}, helperArgs...)...)
	stdoutBuf := boundedCompanionOutput{}
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = nil

	runErr := cmd.Run()
	cmdExitOk := (runErr == nil)

	data := stdoutBuf.Bytes()
	if len(data) == 0 || len(data) > 4096 {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var hr helperResponse
	if err := dec.Decode(&hr); err != nil {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	// Ensure single JSON value, no trailing non-whitespace data
	if dec.More() {
		emitError(out, "companion_data_unavailable")
		return 1
	}
	var dummy interface{}
	if err := dec.Decode(&dummy); err != io.EOF {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	if hr.SchemaVersion == nil || *hr.SchemaVersion != 1 || hr.Ok == nil {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	const maxSafeInt = int64(9007199254740991)

	if !*hr.Ok {
		if cmdExitOk {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		if hr.Error == nil || hr.Action != nil || hr.RetentionDays != nil || hr.Deleted != nil || hr.StoredSpans != nil {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		errCode := *hr.Error
		if errCode != "companion_store_unavailable" &&
			errCode != "companion_retention_invalid" &&
			errCode != "companion_clock_invalid" &&
			errCode != "companion_data_unavailable" {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		emitError(out, errCode)
		return 1
	}

	if !cmdExitOk {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	if hr.Action == nil || *hr.Action != expectedAction {
		emitError(out, "companion_data_unavailable")
		return 1
	}

	if hr.Error != nil || hr.RetentionDays == nil || *hr.RetentionDays < 0 || *hr.RetentionDays > 365 {
		emitError(out, "companion_data_unavailable")
		return 1
	}
	outResp := companionOutput{Action: hr.Action, RetentionDays: hr.RetentionDays}
	if expectedAction == "status" {
		if hr.StoredSpans == nil || *hr.StoredSpans < 0 || *hr.StoredSpans > maxSafeInt || hr.Deleted != nil {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		outResp.StoredSpans = hr.StoredSpans
	} else {
		if hr.Deleted == nil || *hr.Deleted < 0 || *hr.Deleted > maxSafeInt || hr.StoredSpans != nil {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		if reqDays != nil && int(*hr.RetentionDays) != *reqDays {
			emitError(out, "companion_data_unavailable")
			return 1
		}
		outResp.Deleted = hr.Deleted
	}
	if !emitSuccess(out, outResp) {
		return 1
	}
	return 0
}

// Cap output while the helper runs, rather than after buffering arbitrary data.
type boundedCompanionOutput struct{ bytes.Buffer }

func (b *boundedCompanionOutput) Write(p []byte) (int, error) {
	if len(p) > 4096-b.Len() {
		return 0, fmt.Errorf("companion output exceeds limit")
	}
	return b.Buffer.Write(p)
}
