package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const memoryUsage = `Usage: nexus memory list
       nexus memory show <project> [<name>]
       nexus memory save <project> --title "..." [--body "..." | --file <path>] [--tags a,b]
       nexus memory search <query>
       nexus memory init <project>`

// runMemoryCLI returns an exit code and accepts streams/root explicitly for tests.
func runMemoryCLI(args []string, root string, in io.Reader, out, stderr io.Writer) int {
	usage := func(message string) int {
		if message != "" {
			fmt.Fprintln(stderr, message)
		}
		fmt.Fprintln(stderr, memoryUsage)
		return 2
	}
	if len(args) == 0 {
		return usage("")
	}
	var err error
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usage("")
		}
		var projects []memoryProject
		projects, err = listProjects(root)
		if err == nil {
			if len(projects) == 0 {
				fmt.Fprintln(out, "No project memories yet. Run nexus memory init <project>.")
			}
			for _, p := range projects {
				modified := "—"
				if !p.Modified.IsZero() {
					modified = p.Modified.Format(time.RFC3339)
				}
				fmt.Fprintf(out, "%s\t%d files\t%s\n", p.Name, p.Count, modified)
			}
		}
	case "show":
		if len(args) < 2 || len(args) > 3 || !validMemoryComponent(args[1]) {
			return usage("")
		}
		if len(args) == 3 {
			if !validMemoryComponent(args[2]) {
				return usage("")
			}
			var e memoryEntry
			e, err = readMemory(root, args[1], args[2])
			if err == nil {
				_, err = io.WriteString(out, e.Content)
			}
		} else {
			var entries []memoryEntry
			entries, err = listMemories(root, args[1])
			if err == nil {
				if len(entries) == 0 {
					fmt.Fprintln(out, "No memories yet. Run nexus memory save "+args[1]+" --title \"...\" --body \"...\".")
				}
				for _, e := range entries {
					fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", e.Filename, e.Title, e.Date, e.Preview)
				}
			}
		}
	case "save":
		if len(args) < 2 || !validMemoryComponent(args[1]) {
			return usage("")
		}
		flags := flag.NewFlagSet("memory save", flag.ContinueOnError)
		flags.SetOutput(stderr)
		title := flags.String("title", "", "Memory title")
		body := flags.String("body", "", "Inline body")
		file := flags.String("file", "", "Import body from file")
		tags := flags.String("tags", "", "Comma-separated tags")
		if flags.Parse(args[2:]) != nil {
			return usage("")
		}
		if flags.NArg() != 0 || strings.TrimSpace(*title) == "" {
			return usage("--title is required; unexpected positional arguments are not accepted")
		}
		hasBody, hasFile := false, false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "body" {
				hasBody = true
			}
			if f.Name == "file" {
				hasFile = true
			}
		})
		if hasBody && hasFile {
			return usage("--body and --file are mutually exclusive")
		}
		content := *body
		if !hasBody {
			var data []byte
			if hasFile {
				data, err = os.ReadFile(*file)
			} else {
				data, err = io.ReadAll(in)
			}
			content = string(data)
		}
		var tagList []string
		for _, tag := range strings.Split(*tags, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tagList = append(tagList, tag)
			}
		}
		if err == nil {
			var path string
			path, err = saveMemory(root, args[1], *title, content, tagList, time.Now())
			if err == nil {
				fmt.Fprintln(out, path)
			}
		}
	case "search":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return usage("")
		}
		var matches []memoryMatch
		matches, err = searchMemories(root, args[1])
		if err == nil {
			for _, match := range matches {
				fmt.Fprintf(out, "%s/%s: %s\n", match.Project, match.Filename, match.Line)
			}
		}
	case "init":
		if len(args) != 2 || !validMemoryComponent(args[1]) {
			return usage("")
		}
		var path string
		path, err = initProject(root, args[1])
		if err == nil {
			fmt.Fprintln(out, path)
		}
	default:
		return usage("Unknown memory command: " + args[0])
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "Memory project or file does not exist:", err)
		} else {
			fmt.Fprintln(stderr, "NEXUS memory:", err)
		}
		return 1
	}
	return 0
}
