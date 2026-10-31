package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type personaTransport func(*http.Request) (*http.Response, error)

func (f personaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testPersona = "---\nname: Reviewer\ndescription: Reviews code safely\n---\n\nReview the code.\n"

func personaFixture() personaCatalog {
	return personaCatalog{Version: "1.0.0", Count: 1, Personas: []personaEntry{{ID: "reviewer", Name: "Reviewer", Description: "Reviews code safely", Path: "personas/reviewer.md", RawURL: "https://raw.githubusercontent.com/canoo/Nexus-Personas/main/personas/reviewer.md"}}}
}
func blobMetadata(path string, body []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(body))
	h.Write(body)
	b, _ := json.Marshal(map[string]any{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body), "sha": hex.EncodeToString(h.Sum(nil))})
	return b
}
func setPersonaHTTP(t *testing.T, handler personaTransport) {
	t.Helper()
	old := personaHTTPClient
	t.Cleanup(func() { personaHTTPClient = old })
	personaHTTPClient = &http.Client{Transport: handler, Timeout: old.Timeout, CheckRedirect: personaRedirect}
}
func fakePersonaSource(t *testing.T, c personaCatalog, content string) {
	t.Helper()
	setPersonaHTTP(t, func(r *http.Request) (*http.Response, error) {
		if !validPersonaAPI(r.URL) {
			t.Fatalf("untrusted request: %s", r.URL)
		}
		path := strings.TrimPrefix(r.URL.Path, "/repos/canoo/Nexus-Personas/contents/")
		var b []byte
		switch path {
		case "registry.json":
			b, _ = json.Marshal(c)
		case "personas/reviewer.md":
			b = []byte(content)
		default:
			t.Fatalf("unexpected path %s", path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(blobMetadata(path, b))), Request: r}, nil
	})
}
func TestPersonaListInstallAndPreserve(t *testing.T) {
	fakePersonaSource(t, personaFixture(), testPersona)
	root, home := t.TempDir(), t.TempDir()
	run := func(args ...string) (int, string) {
		var out, stderr bytes.Buffer
		code := runPersonaCLI(args, root, home, &out, &stderr)
		return code, out.String() + stderr.String()
	}
	if code, out := run("list", "--json"); code != 0 || !strings.Contains(out, `"id":"reviewer"`) {
		t.Fatalf("list: %d %s", code, out)
	}
	for _, args := range [][]string{{"install", "reviewer", "--json"}, {"install", "reviewer", "--global", "--json"}} {
		if code, out := run(args...); code != 0 || !strings.Contains(out, `"changed":true`) {
			t.Fatalf("install: %d %s", code, out)
		}
		if code, out := run(args...); code != 0 || !strings.Contains(out, `"changed":false`) {
			t.Fatalf("idempotence: %d %s", code, out)
		}
	}
	project := filepath.Join(root, ".agents", "skills", "reviewer", "SKILL.md")
	if e := os.WriteFile(project, []byte("local edits"), 0600); e != nil {
		t.Fatal(e)
	}
	if code, out := run("install", "reviewer", "--json"); code != 1 || !strings.Contains(out, "different content") {
		t.Fatalf("overwrite: %d %s", code, out)
	}
	b, _ := os.ReadFile(project)
	if string(b) != "local edits" {
		t.Fatal("local edits overwritten")
	}
	if code, _ := run("install", "absent"); code != 1 {
		t.Fatal("unknown ID accepted")
	}
}
func TestPersonaRejectsRegistryAndContent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*personaCatalog)
		content string
	}{
		{"version", func(c *personaCatalog) { c.Version = "9" }, testPersona},
		{"count", func(c *personaCatalog) { c.Count = 2 }, testPersona},
		{"id", func(c *personaCatalog) { c.Personas[0].ID = "../escape" }, testPersona},
		{"path", func(c *personaCatalog) { c.Personas[0].Path = "personas/../secret" }, testPersona},
		{"raw host", func(c *personaCatalog) {
			c.Personas[0].RawURL = "https://evil.example/canoo/Nexus-Personas/main/personas/reviewer.md"
		}, testPersona},
		{"raw repository", func(c *personaCatalog) {
			c.Personas[0].RawURL = "https://raw.githubusercontent.com/evil/Nexus-Personas/main/personas/reviewer.md"
		}, testPersona},
		{"raw path", func(c *personaCatalog) {
			c.Personas[0].RawURL = "https://raw.githubusercontent.com/canoo/Nexus-Personas/main/personas/other.md"
		}, testPersona},
		{"duplicate", func(c *personaCatalog) { c.Personas = append(c.Personas, c.Personas[0]); c.Count++ }, testPersona},
		{"sha256", func(c *personaCatalog) { c.Personas[0].SHA256 = strings.Repeat("0", 64) }, testPersona},
		{"frontmatter", nil, "No frontmatter"},
		{"name type", nil, "---\nname: 123\ndescription: Reviews code safely\n---\nBody\n"},
		{"allowedTools type", nil, strings.Replace(testPersona, "description: Reviews code safely", "description: Reviews code safely\nallowedTools: 5", 1)},
		{"color type", nil, strings.Replace(testPersona, "description: Reviews code safely", "description: Reviews code safely\ncolor: [blue]", 1)},
		{"name mismatch", nil, strings.Replace(testPersona, "Reviewer", "Impostor", 1)},
		{"description mismatch", nil, strings.Replace(testPersona, "Reviews code safely", "Other", 1)},
		{"missing description", nil, "---\nname: Reviewer\n---\nBody\n"},
		{"empty body", nil, "---\nname: Reviewer\ndescription: Reviews code safely\n---\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := personaFixture()
			if tt.mutate != nil {
				tt.mutate(&c)
			}
			fakePersonaSource(t, c, tt.content)
			root := t.TempDir()
			var out bytes.Buffer
			if code := runPersonaCLI([]string{"install", "reviewer", "--json"}, root, t.TempDir(), &out, &out); code != 1 || !strings.Contains(out.String(), `"error"`) {
				t.Fatalf("accepted invalid source: %d %s", code, out.String())
			}
			if _, e := os.Stat(filepath.Join(root, ".agents")); !os.IsNotExist(e) {
				t.Fatal("invalid source created destination")
			}
		})
	}
}
func TestPersonaHTTPIntegrityBoundsAndFailures(t *testing.T) {
	for _, name := range []string{"hash", "oversize", "content oversize", "status", "network", "malformed", "type"} {
		t.Run(name, func(t *testing.T) {
			setPersonaHTTP(t, func(r *http.Request) (*http.Response, error) {
				if name == "network" {
					return nil, fmt.Errorf("offline")
				}
				raw := blobMetadata("registry.json", []byte("{}"))
				status := 200
				switch name {
				case "hash":
					var m map[string]any
					json.Unmarshal(raw, &m)
					m["sha"] = strings.Repeat("0", 40)
					raw, _ = json.Marshal(m)
				case "content oversize":
					raw = blobMetadata("registry.json", bytes.Repeat([]byte("x"), personaLimit+1))
				case "oversize":
					raw = bytes.Repeat([]byte("x"), 2*personaLimit+1)
				case "status":
					status = 429
				case "malformed":
					raw = []byte("{")
				case "type":
					var m map[string]any
					json.Unmarshal(raw, &m)
					m["type"] = "symlink"
					raw, _ = json.Marshal(m)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
			})
			if _, e := fetchPersonaBlob(context.Background(), "registry.json"); e == nil {
				t.Fatal("invalid HTTP result accepted")
			}
			var out bytes.Buffer
			if code := runPersonaCLI([]string{"list", "--json"}, t.TempDir(), t.TempDir(), &out, &out); code != 1 || !strings.Contains(out.String(), `"error"`) {
				t.Fatalf("error output: %d %s", code, &out)
			}
		})
	}
}
func TestPersonaRedirectValidation(t *testing.T) {
	for _, s := range []string{"http://api.github.com/repos/canoo/Nexus-Personas/contents/registry.json", "https://evil.example/file", "https://api.github.com/repos/evil/Nexus-Personas/contents/file", "https://api.github.com/repos/canoo/Nexus-Personas/contents/file?ref=evil"} {
		u, _ := url.Parse(s)
		if personaRedirect(&http.Request{URL: u}, nil) == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	u, _ := url.Parse(personaAPI + "registry.json")
	if e := personaRedirect(&http.Request{URL: u}, nil); e != nil {
		t.Fatal(e)
	}
	if e := personaRedirect(&http.Request{URL: u}, make([]*http.Request, 3)); e == nil {
		t.Fatal("redirect limit not enforced")
	}
}
func TestPersonaSymlinksAndInvalidCLI(t *testing.T) {
	fakePersonaSource(t, personaFixture(), testPersona)
	for _, kind := range []string{"parent", "file"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			var link, target string
			if kind == "parent" {
				link = filepath.Join(root, ".agents")
				target = outside
			} else {
				parent := filepath.Join(root, ".agents", "skills", "reviewer")
				os.MkdirAll(parent, 0700)
				link = filepath.Join(parent, "SKILL.md")
				target = filepath.Join(outside, "persona.md")
				os.WriteFile(target, []byte(testPersona), 0600)
			}
			if e := os.Symlink(target, link); e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			if code := runPersonaCLI([]string{"install", "reviewer"}, root, t.TempDir(), &out, &out); code != 1 {
				t.Fatalf("symlink accepted: %s", &out)
			}
		})
	}
	for _, args := range [][]string{nil, {"install", "../escape"}, {"list", "--global"}, {"install"}, {"list", "extra"}, {"install", "reviewer", "--unknown"}} {
		var out bytes.Buffer
		if code := runPersonaCLI(args, t.TempDir(), t.TempDir(), &out, &out); code != 2 {
			t.Fatalf("invalid args accepted: %v: %s", args, &out)
		}
	}
}

func TestPersonaRegistryBuilderDescriptionCompatibility(t *testing.T) {
	for _, tt := range []struct{ description, content string }{
		{`First line`, "---\nname: Reviewer\ndescription: \"First line\n\nMore context\"\n---\nBody\n"},
		{`First\nsecond`, "---\nname: Reviewer\ndescription: \"First\\nsecond\"\n---\nBody\n"},
	} {
		p := personaFixture().Personas[0]
		p.Description = tt.description
		if e := validatePersonaContent(p, []byte(tt.content)); e != nil {
			t.Fatal(e)
		}
	}
}

func TestPersonaClientBlocksForeignRedirect(t *testing.T) {
	calls := 0
	setPersonaHTTP(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/persona"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, e := fetchPersonaBlob(context.Background(), "registry.json"); e == nil {
		t.Fatal("foreign redirect accepted")
	}
	if calls != 1 {
		t.Fatalf("foreign server contacted: %d", calls)
	}
}
