package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type limitedOutput interface {
	io.Writer
	Len() int
}

func TestOutputLimitsThroughIOCopy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		new   func() limitedOutput
	}{
		{"control", controlMaxOutput, func() limitedOutput { return &controlBuffer{} }},
		{"companion", 4096, func() limitedOutput { return &boundedCompanionOutput{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.new()
			// Hide bytes.Reader.WriteTo, forcing io.Copy's destination optimization path.
			source := struct{ io.Reader }{bytes.NewReader(bytes.Repeat([]byte("x"), tc.limit+1))}
			if _, err := io.Copy(out, source); err == nil {
				t.Fatal("io.Copy bypassed output limit")
			}
			if out.Len() > tc.limit {
				t.Fatal("retained oversized output")
			}
		})
	}
}
func TestBoundedOutputSubprocessHelper(t *testing.T) {
	if os.Getenv("NEXUS_BOUNDED_OUTPUT_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), controlMaxOutput+1))
	os.Exit(0)
}
func TestOutputLimitsThroughRealSubprocess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		out   limitedOutput
	}{
		{"control", controlMaxOutput, &controlBuffer{}}, {"companion", 4096, &boundedCompanionOutput{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBoundedOutputSubprocessHelper$")
			cmd.Env = append(os.Environ(), "NEXUS_BOUNDED_OUTPUT_HELPER=1")
			configureRouteProcess(cmd)
			cmd.Stdout = tc.out
			if err := cmd.Run(); err == nil {
				t.Fatal("subprocess bypassed output limit")
			}
			if tc.out.Len() > tc.limit {
				t.Fatal("retained oversized output")
			}
		})
	}
}
