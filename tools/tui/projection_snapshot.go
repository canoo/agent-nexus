package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type instructionBackup struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	Content      []byte `json:"content_base64"`
	Mode         uint32 `json:"mode"`
	ExpectedHash string `json:"expected_sha256"`
}
type instructionSnapshot struct {
	Version int                 `json:"version"`
	ID      string              `json:"id"`
	Files   []instructionBackup `json:"files"`
}

const latestInstructionSnapshot = ".nexus/.snapshots/latest.json"

func snapshotInstructions(root string, writes []projectionWrite) error {
	snapshot := instructionSnapshot{Version: 1, ID: time.Now().UTC().Format("20060102T150405.000000000Z")}
	for _, w := range writes {
		if strings.HasPrefix(w.Path, ".nexus/.adopted/") || w.Path == ".nexus/.gitignore" {
			continue
		} // originals and protective ignores remain after rollback
		old, e := instructionRead(root, w.Path)
		if e != nil {
			return e
		}
		mode := uint32(0644)
		if info, e := os.Stat(filepath.Join(root, w.Path)); e == nil {
			mode = uint32(info.Mode().Perm())
		}
		snapshot.Files = append(snapshot.Files, instructionBackup{w.Path, old != nil, old, mode, instructionHash(w.Content)})
	}
	if len(snapshot.Files) == 0 {
		return nil
	}
	b, e := json.MarshalIndent(snapshot, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	// Snapshot data can include private project instructions. Snapshot storage is
	// deliberately private and must be ignored by version control.
	for _, rel := range []string{".nexus/.snapshots/" + snapshot.ID + ".json", latestInstructionSnapshot} {
		if _, e = instructionRead(root, rel); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0700); e != nil {
			return e
		}
		if e = writeInstructionAtomic(filepath.Join(root, rel), b, 0600); e != nil {
			return e
		}
	}
	return nil
}
func writeInstructionAtomic(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".nexus-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	e = f.Chmod(mode)
	if e == nil {
		_, e = f.Write(b)
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(tmp, path)
	}
	return e
}
func rollbackInstructions(root string, dry bool, out io.Writer) error {
	resolved, e := filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	root, e = filepath.Abs(resolved)
	if e != nil {
		return e
	}
	b, e := instructionRead(root, latestInstructionSnapshot)
	if e != nil {
		return e
	}
	if b == nil {
		return fmt.Errorf("no active snapshot to roll back")
	}
	var snapshot instructionSnapshot
	if e = json.Unmarshal(b, &snapshot); e != nil {
		return fmt.Errorf("invalid snapshot: %w", e)
	}
	if snapshot.Version != 1 || len(snapshot.Files) == 0 {
		return fmt.Errorf("invalid snapshot version or empty files")
	}
	seen := map[string]bool{}
	for _, entry := range snapshot.Files {
		// Never trust paths from a disk-editable manifest for writes/deletions.
		allowed := entry.Path == instructionPath || entry.Path == projectionStatePath || entry.Path == "AGENTS.validation.json" || entry.Path == ".nexus/.gitignore"
		for _, d := range instructionDrivers {
			if d.Path == entry.Path {
				allowed = true
			}
		}
		if !allowed || seen[entry.Path] {
			return fmt.Errorf("invalid snapshot path: %s", entry.Path)
		}
		seen[entry.Path] = true
		current, e := instructionRead(root, entry.Path)
		if e != nil {
			return e
		}
		if current == nil || instructionHash(current) != entry.ExpectedHash {
			return fmt.Errorf("%s changed since sync; refusing rollback over local edits", entry.Path)
		}
	}
	for _, entry := range snapshot.Files {
		if dry {
			fmt.Fprintln(out, "would restore "+entry.Path)
			continue
		}
		path := filepath.Join(root, entry.Path)
		if entry.Existed {
			e = writeInstructionAtomic(path, entry.Content, os.FileMode(entry.Mode).Perm())
		} else {
			e = os.Remove(path)
		}
		if e != nil {
			return fmt.Errorf("restore %s: %w (earlier files may have been restored)", entry.Path, e)
		}
		fmt.Fprintln(out, "restored "+entry.Path)
	}
	if !dry {
		return os.Remove(filepath.Join(root, latestInstructionSnapshot))
	}
	return nil
}
