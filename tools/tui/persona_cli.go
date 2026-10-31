package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const personaAPI = "https://api.github.com/repos/canoo/Nexus-Personas/contents/"
const personaLimit = 1 << 20

var personaID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
var personaHTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: personaRedirect}

type personaEntry struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Path         string   `json:"path"`
	RawURL       string   `json:"rawUrl"`
	SHA256       string   `json:"sha256,omitempty"`
	Emoji        string   `json:"emoji,omitempty"`
	Color        string   `json:"color,omitempty"`
	Vibe         string   `json:"vibe,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
}
type personaCatalog struct {
	Version  string         `json:"version"`
	Count    int            `json:"count"`
	Personas []personaEntry `json:"personas"`
}

func personaRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || !validPersonaAPI(req.URL) {
		return errors.New("persona redirect outside trusted repository")
	}
	return nil
}
func validPersonaAPI(u *url.URL) bool {
	if u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	const prefix = "/repos/canoo/Nexus-Personas/contents/"
	if !strings.HasPrefix(u.Path, prefix) {
		return false
	}
	path := strings.TrimPrefix(u.Path, prefix)
	if path == "registry.json" {
		return true
	}
	if !strings.HasPrefix(path, "personas/") || !strings.HasSuffix(path, ".md") {
		return false
	}
	return personaID.MatchString(strings.TrimSuffix(strings.TrimPrefix(path, "personas/"), ".md"))
}

// The contents API resolves the repository default branch and supplies the Git
// blob SHA. This checks byte integrity, not an independent publisher signature.
func fetchPersonaBlob(ctx context.Context, path string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, personaAPI+path, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nexus-persona-manager")
	res, e := personaHTTPClient.Do(req)
	if e != nil {
		return nil, fmt.Errorf("fetch persona source: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("persona source returned HTTP %d", res.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 2*personaLimit+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 2*personaLimit {
		return nil, errors.New("persona API response exceeds limit")
	}
	var blob struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Path     string `json:"path"`
	}
	if e = json.Unmarshal(raw, &blob); e != nil {
		return nil, fmt.Errorf("invalid persona API metadata: %w", e)
	}
	if blob.Type != "file" || blob.Encoding != "base64" || blob.Path != path {
		return nil, errors.New("invalid persona file metadata")
	}
	content, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if e != nil {
		return nil, e
	}
	if len(content) > personaLimit {
		return nil, errors.New("persona content exceeds limit")
	}
	hash := sha1.New()
	fmt.Fprintf(hash, "blob %d\x00", len(content))
	hash.Write(content)
	if len(blob.SHA) != 40 || hex.EncodeToString(hash.Sum(nil)) != blob.SHA {
		return nil, errors.New("persona Git blob integrity mismatch")
	}
	return content, nil
}

func loadPersonaCatalog(ctx context.Context) (personaCatalog, error) {
	var c personaCatalog
	b, e := fetchPersonaBlob(ctx, "registry.json")
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("invalid persona registry: %w", e)
	}
	if c.Version != "1.0.0" || c.Count != len(c.Personas) || c.Personas == nil {
		return c, errors.New("unsupported or invalid persona registry schema")
	}
	seen := map[string]bool{}
	for _, p := range c.Personas {
		u, e := url.Parse(p.RawURL)
		if !personaID.MatchString(p.ID) || seen[p.ID] || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Description) == "" || p.Path != "personas/"+p.ID+".md" {
			return c, errors.New("invalid persona registry entry")
		}
		if e != nil || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
			return c, errors.New("untrusted persona rawUrl")
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 5 || parts[0] != "canoo" || parts[1] != "Nexus-Personas" || parts[2] == "" || parts[2] == "." || parts[2] == ".." || strings.Join(parts[3:], "/") != p.Path {
			return c, errors.New("persona rawUrl must name the official repository and matching path")
		}
		if p.SHA256 != "" {
			if digest, e := hex.DecodeString(p.SHA256); e != nil || len(digest) != 32 {
				return c, errors.New("invalid registry sha256")
			}
		}
		seen[p.ID] = true
	}
	sort.Slice(c.Personas, func(i, j int) bool { return c.Personas[i].ID < c.Personas[j].ID })
	return c, nil
}

func validatePersonaFrontmatter(b []byte) error {
	var doc yaml.Node
	if e := yaml.Unmarshal(b, &doc); e != nil {
		return e
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("frontmatter must be a mapping")
	}
	fields := doc.Content[0].Content
	for i := 0; i < len(fields); i += 2 {
		key, value := fields[i].Value, fields[i+1]
		switch key {
		case "name", "description", "emoji", "color", "vibe":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return fmt.Errorf("frontmatter %s must be a string", key)
			}
		case "allowedTools":
			if value.Kind != yaml.SequenceNode {
				return errors.New("frontmatter allowedTools must be an array")
			}
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
					return errors.New("frontmatter allowedTools entries must be strings")
				}
			}
		}
	}
	return nil
}

func validatePersonaContent(p personaEntry, b []byte) error {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return errors.New("persona requires YAML frontmatter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return errors.New("persona frontmatter is not closed")
	}
	if e := validatePersonaFrontmatter([]byte(text[4 : 4+end])); e != nil {
		return fmt.Errorf("invalid persona schema: %w", e)
	}
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if e := yaml.Unmarshal([]byte(text[4:4+end]), &fm); e != nil {
		return fmt.Errorf("invalid persona frontmatter: %w", e)
	}
	desc := p.Description
	// Registry v1's builder preserves quoted YAML escapes in its description.
	if decoded, e := strconv.Unquote(`"` + desc + `"`); e == nil {
		desc = decoded
	}
	// Upstream v1 also truncates a physically multiline quoted description to
	// its first source line. Compare that exact builder representation, not an
	// arbitrary prefix of the parsed description.
	builderDescription := ""
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if strings.HasPrefix(line, "description:") {
			builderDescription = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			builderDescription = strings.TrimPrefix(strings.TrimPrefix(builderDescription, `"`), "'")
			builderDescription = strings.TrimSuffix(strings.TrimSuffix(builderDescription, `"`), "'")
			break
		}
	}
	if fm.Name != p.Name || strings.TrimSpace(fm.Description) == "" || (fm.Description != desc && p.Description != builderDescription) {
		return errors.New("persona frontmatter does not match registry name/description")
	}
	if strings.TrimSpace(text[4+end+5:]) == "" {
		return errors.New("persona body is empty")
	}
	return verifyPersonaDigest(p, b)
}

func verifyPersonaDigest(p personaEntry, b []byte) error {
	if p.SHA256 != "" {
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != strings.ToLower(p.SHA256) {
			return errors.New("persona sha256 integrity mismatch")
		}
	}
	return nil
}

func writePersona(root, rel string, b []byte) (bool, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return false, e
	}
	// Reject existing links before opening a confined root. Root methods also
	// prevent a concurrent path change from escaping the selected destination.
	if _, e = instructionRead(root, rel); e != nil {
		return false, e
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return false, e
	}
	defer r.Close()
	parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	current := ""
	for _, part := range parts {
		current = filepath.Join(current, part)
		if e = r.Mkdir(current, 0700); e != nil && !os.IsExist(e) {
			return false, e
		}
		info, e := r.Lstat(current)
		if e != nil {
			return false, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("persona destination must contain only directories, without symlinks")
		}
	}
	f, e := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(e) {
		info, err := r.Lstat(rel)
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("persona destination is not a regular file")
		}
		old, err := r.ReadFile(rel)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, b) {
			return false, nil
		}
		return false, errors.New("persona destination already exists with different content; preserve or remove it manually")
	}
	if e != nil {
		return false, e
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		_ = r.Remove(rel)
		return false, e
	}
	return true, nil
}

func runPersonaCLI(args []string, dir, home string, out, stderr io.Writer) int {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}
	fail := func(code int, e error) int {
		if jsonOutput {
			_ = json.NewEncoder(out).Encode(map[string]string{"error": e.Error()})
		} else {
			fmt.Fprintln(stderr, "persona:", e)
		}
		return code
	}
	if len(args) == 0 || (args[0] != "list" && args[0] != "install") {
		return fail(2, errors.New("usage: nexus persona list [--json] | install <id> [--global] [--json]"))
	}
	fs := flag.NewFlagSet("persona "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	global := fs.Bool("global", false, "install to user persona registry")
	asJSON := fs.Bool("json", false, "JSON output")
	// Permit flags after the install ID, as shown in the help text.
	rest := args[1:]
	id := ""
	if args[0] == "install" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		id = rest[0]
		rest = rest[1:]
	}
	if e := fs.Parse(rest); e != nil {
		return fail(2, e)
	}
	jsonOutput = *asJSON
	if args[0] == "install" && id == "" && fs.NArg() == 1 {
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fail(2, errors.New("unexpected persona arguments"))
	}
	if args[0] == "install" && !personaID.MatchString(id) {
		return fail(2, errors.New("install requires a safe persona ID (lowercase letters, digits, hyphens)"))
	}
	if args[0] == "list" && *global {
		return fail(2, errors.New("--global applies only to install"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog, e := loadPersonaCatalog(ctx)
	if e != nil {
		return fail(1, e)
	}
	if args[0] == "list" {
		if jsonOutput {
			_ = json.NewEncoder(out).Encode(catalog)
		} else {
			for _, p := range catalog.Personas {
				fmt.Fprintf(out, "%s\t%s\n", p.ID, p.Name)
			}
		}
		return 0
	}
	var selected *personaEntry
	for i := range catalog.Personas {
		if catalog.Personas[i].ID == id {
			selected = &catalog.Personas[i]
			break
		}
	}
	if selected == nil {
		return fail(1, fmt.Errorf("persona %q is not in the official registry", id))
	}
	b, e := fetchPersonaBlob(ctx, selected.Path)
	if e != nil {
		return fail(1, e)
	}
	if e = validatePersonaContent(*selected, b); e != nil {
		return fail(1, e)
	}
	root, rel := dir, filepath.Join(".agents", "skills", id, "SKILL.md")
	if *global {
		if home == "" {
			return fail(1, errors.New("cannot locate user home"))
		}
		root = home
		rel = filepath.Join(".config", "nexus", "personas", id+".md")
	}
	changed, e := writePersona(root, rel, b)
	if e != nil {
		return fail(1, e)
	}
	path := filepath.Join(root, rel)
	if jsonOutput {
		_ = json.NewEncoder(out).Encode(map[string]any{"id": id, "path": path, "changed": changed, "integrity": "git-blob-sha1"})
	} else {
		fmt.Fprintf(out, "Persona %s ready at %s (changed: %t)\n", id, path, changed)
	}
	return 0
}
