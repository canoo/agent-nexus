package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

type memoryProject struct {
	Name     string
	Count    int
	Modified time.Time
}
type memoryEntry struct {
	Filename, Title, Date, Preview, Body, Content string
	Modified                                      time.Time
}
type memoryMatch struct{ Project, Filename, Line string }
type memoryMetadata struct {
	Title string   `yaml:"title"`
	Date  string   `yaml:"date"`
	Tags  []string `yaml:"tags,omitempty"`
}

func memoryRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "nexus", "agent-memory"), nil
}

// Names are single path components; memories never escape their project directory.
func validMemoryComponent(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00\r\n") && !filepath.IsAbs(name)
}
func projectPath(root, project string) (string, error) {
	if !validMemoryComponent(project) {
		return "", fmt.Errorf("invalid project name %q", project)
	}
	path := filepath.Join(root, project)
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return "", fmt.Errorf("project is not a regular directory: %s", project)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
func memoryPath(root, project, name string) (string, error) {
	dir, err := projectPath(root, project)
	if err != nil {
		return "", err
	}
	if !validMemoryComponent(name) {
		return "", fmt.Errorf("invalid memory name %q", name)
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("memory is not a regular file: %s", name)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
func listProjects(root string) ([]memoryProject, error) {
	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []memoryProject
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries, err := listMemories(root, dir.Name())
		if err != nil {
			return nil, err
		}
		p := memoryProject{Name: dir.Name(), Count: len(entries)}
		for _, e := range entries {
			if e.Modified.After(p.Modified) {
				p.Modified = e.Modified
			}
		}
		result = append(result, p)
	}
	return result, nil
}
func listMemories(root, project string) ([]memoryEntry, error) {
	dir, err := projectPath(root, project)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []memoryEntry
	for _, file := range files {
		if file.Type()&os.ModeSymlink != 0 || file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
			continue
		}
		e, err := readMemory(root, project, file.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}
func parseMemory(filename, content string) memoryEntry {
	e := memoryEntry{Filename: filename, Content: content, Body: content}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.HasPrefix(normalized, "---\n") {
		lines := strings.Split(normalized, "\n")
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" || lines[i] == "..." {
				var meta memoryMetadata
				if yaml.Unmarshal([]byte(strings.Join(lines[1:i], "\n")), &meta) == nil {
					e.Title = meta.Title
					e.Date = meta.Date
					e.Body = strings.Join(lines[i+1:], "\n")
				}
				break
			}
		}
	}
	for _, line := range strings.Split(e.Body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && e.Preview == "" {
			e.Preview = line
		}
		if e.Title == "" && strings.HasPrefix(line, "# ") {
			e.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	if e.Title == "" {
		e.Title = filename
	}
	return e
}
func readMemory(root, project, name string) (memoryEntry, error) {
	path, err := memoryPath(root, project, name)
	if err != nil {
		return memoryEntry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return memoryEntry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return memoryEntry{}, err
	}
	e := parseMemory(filepath.Base(path), string(data))
	e.Modified = info.ModTime()
	return e, nil
}
func memorySlug(title string) string {
	var out strings.Builder
	separator := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	if out.Len() == 0 {
		return "memory"
	}
	slug := out.String()
	runes := []rune(slug)
	if len(runes) > 80 {
		slug = strings.TrimRight(string(runes[:80]), "-")
	}
	return slug
}
func saveMemory(root, project, title, body string, tags []string, now time.Time) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", errors.New("title is required")
	}
	dir, err := projectPath(root, project)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	metadata, err := yaml.Marshal(memoryMetadata{Title: title, Date: now.Format(time.RFC3339), Tags: tags})
	if err != nil {
		return "", err
	}
	content := "---\n" + string(metadata) + "---\n\n" + body
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	// Preserve the prescribed filename format without overwriting a same-second save.
	for i := 0; i < 3600; i++ {
		stamp := now.Add(time.Duration(i) * time.Second)
		path := filepath.Join(dir, stamp.Format("20060102-150405")+"-"+memorySlug(title)+".md")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.WriteString(content)
		closeErr := f.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		return path, nil
	}
	return "", errors.New("too many filename collisions")
}
func searchMemories(root, query string) ([]memoryMatch, error) {
	projects, err := listProjects(root)
	if err != nil {
		return nil, err
	}
	var result []memoryMatch
	for _, p := range projects {
		entries, err := listMemories(root, p.Name)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			for _, line := range strings.Split(e.Content, "\n") {
				if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
					result = append(result, memoryMatch{p.Name, e.Filename, line})
				}
			}
		}
	}
	return result, nil
}
func initProject(root, project string) (string, error) {
	dir, err := projectPath(root, project)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "README.md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	_, err = f.WriteString("# Project memory\n\nStore project decisions, preferences, and blockers as Markdown files in this directory. Optional YAML frontmatter may contain title, date, and tags.\n\nRead with `nexus memory show " + project + "` and `nexus memory show " + project + " <name>`, or read the .md files directly at session start. Save with `nexus memory save " + project + " --title \"Decision\" --body \"Context\"`.\n")
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return path, closeErr
}
func deleteMemory(root, project, name string) error {
	path, err := memoryPath(root, project, name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
