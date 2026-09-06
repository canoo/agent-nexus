package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var settingDefaults = map[string]string{"NEXUS_LOCAL_AI": "true", "OLLAMA_HOST_URL": "http://localhost:11434", "NEXUS_SUPERVISOR_MODEL": "qwen2.5-coder:1.5b", "NEXUS_LOGIC_MODEL": "llama3.2:3b"}
var settingLine = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*(.*?)[ \t\r]*$`)

// Values are literal single-line strings: no interpolation or shell evaluation.
func parseSettings(data string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		match := settingLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value := match[2]
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		values[match[1]] = value
	}
	return values
}

func writeSettings(path string, keys, values []string) error {
	updates := map[string]string{}
	for i, key := range keys {
		value := values[i]
		if strings.ContainsAny(value, "\r\n\x00\"'\\`$") {
			return fmt.Errorf("%s contains unsupported settings characters", key)
		}
		updates[key] = key + "=\"" + value + "\""
	}
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("settings must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	seen := map[string]bool{}
	for i, line := range lines {
		match := settingLine.FindStringSubmatch(line)
		if match != nil {
			if replacement, ok := updates[match[1]]; ok {
				lines[i] = replacement
				seen[match[1]] = true
			}
		}
	}
	for _, key := range keys {
		if !seen[key] {
			lines = append(lines, updates[key])
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nexus-env-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
