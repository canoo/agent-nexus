package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type memoryMode int

const (
	memoryBrowse memoryMode = iota
	memoryRead
	memorySearch
	memoryNewProject
	memoryNewTitle
	memoryNewBody
	memoryDelete
)

type memoryState struct {
	root                                 string
	projects                             []memoryProject
	entries                              []memoryEntry
	projectCursor, memoryCursor, pane    int
	mode                                 memoryMode
	filter, newProject, newTitle, notice string
	err                                  error
	input                                textinput.Model
	body                                 textarea.Model
	reader                               viewport.Model
	height                               int
}

func startMemory(m model) (tea.Model, tea.Cmd) {
	m.screen = screenMemory
	root, err := memoryRoot()
	height := m.height
	if height == 0 {
		height = 24
	}
	m.memory = memoryState{root: root, err: err, height: height}
	if err == nil {
		refreshMemory(&m.memory, "")
	}
	return m, nil
}

func refreshMemory(s *memoryState, selectProject string) {
	projects, err := listProjects(s.root)
	if err != nil {
		s.err = err
		return
	}
	s.projects = projects
	if selectProject != "" {
		for i, p := range projects {
			if p.Name == selectProject {
				s.projectCursor = i
				break
			}
		}
	}
	s.projectCursor = min(s.projectCursor, max(0, len(projects)-1))
	loadMemoryEntries(s)
}

func loadMemoryEntries(s *memoryState) {
	s.entries = nil
	s.memoryCursor = 0
	if len(s.projects) == 0 {
		return
	}
	entries, err := listMemories(s.root, s.projects[s.projectCursor].Name)
	if err != nil {
		s.err = err
		return
	}
	s.err = nil
	for _, entry := range entries {
		if s.filter == "" || strings.Contains(strings.ToLower(entry.Content+"\n"+entry.Filename), strings.ToLower(s.filter)) {
			s.entries = append(s.entries, entry)
		}
	}
}

func memoryInput(s *memoryState, placeholder, value string) tea.Cmd {
	s.input = textinput.New()
	s.input.Placeholder = placeholder
	s.input.SetValue(value)
	s.input.SetWidth(60)
	return s.input.Focus()
}

func updateMemory(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	s := &m.memory
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		s.height = size.Height
		if s.mode == memoryRead {
			s.reader.SetHeight(max(3, size.Height-10))
			s.reader.SetWidth(memoryContentWidth(m))
		}
		s.input.SetWidth(memoryContentWidth(m))
		if s.mode == memoryNewBody {
			s.body.SetWidth(memoryContentWidth(m))
			s.body.SetHeight(max(3, min(6, size.Height-14)))
		}
		return m, nil
	}
	key, isKey := msg.(tea.KeyPressMsg)
	if s.mode == memoryNewProject || s.mode == memoryNewTitle || s.mode == memoryNewBody || s.mode == memorySearch {
		if isKey && key.String() == "esc" {
			if s.mode == memorySearch {
				s.filter = ""
				loadMemoryEntries(s)
			}
			s.mode = memoryBrowse
			return m, nil
		}
		if isKey && ((key.String() == "enter" && s.mode != memoryNewBody) || (key.String() == "ctrl+s" && s.mode == memoryNewBody)) {
			value := strings.TrimSpace(s.input.Value())
			switch s.mode {
			case memorySearch:
				s.mode = memoryBrowse
			case memoryNewProject:
				if value == "" {
					s.err = fmt.Errorf("enter a project name")
					return m, nil
				}
				s.newProject = value
				s.mode = memoryNewTitle
				s.err = nil
				return m, memoryInput(s, "Memory title", "")
			case memoryNewTitle:
				if value == "" {
					s.err = fmt.Errorf("enter a title")
					return m, nil
				}
				s.newTitle = value
				s.mode = memoryNewBody
				s.err = nil
				s.body = textarea.New()
				s.body.Placeholder = "Decisions, preferences, or context for the next session…"
				s.body.SetWidth(memoryContentWidth(m))
				s.body.SetHeight(6)
				return m, s.body.Focus()
			case memoryNewBody:
				path, err := saveMemory(s.root, s.newProject, s.newTitle, s.body.Value(), nil, time.Now())
				if err != nil {
					s.err = err
					return m, nil
				}
				s.mode = memoryBrowse
				s.filter = ""
				s.notice = "Saved " + path
				refreshMemory(s, s.newProject)
			}
			return m, nil
		}
		var cmd tea.Cmd
		if s.mode == memoryNewBody {
			s.body, cmd = s.body.Update(msg)
		} else {
			s.input, cmd = s.input.Update(msg)
			if s.mode == memorySearch {
				s.filter = s.input.Value()
				loadMemoryEntries(s)
			}
		}
		return m, cmd
	}
	if !isKey {
		if s.mode == memoryRead {
			var cmd tea.Cmd
			s.reader, cmd = s.reader.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if key.String() == "q" {
		return m, tea.Quit
	}
	if s.mode == memoryDelete {
		switch key.String() {
		case "y":
			if len(s.entries) > 0 && len(s.projects) > 0 {
				s.err = deleteMemory(s.root, s.projects[s.projectCursor].Name, s.entries[s.memoryCursor].Filename)
				if s.err == nil {
					s.notice = "Memory deleted"
					refreshMemory(s, "")
				}
			}
			s.mode = memoryBrowse
		case "n", "esc":
			s.mode = memoryBrowse
		}
		return m, nil
	}
	if s.mode == memoryRead {
		if key.String() == "esc" || key.String() == "enter" {
			s.mode = memoryBrowse
			return m, nil
		}
		var cmd tea.Cmd
		s.reader, cmd = s.reader.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "esc":
		m.screen = screenMenu
	case "tab":
		s.pane = 1 - s.pane
	case "h", "left":
		s.pane = 0
	case "l", "right":
		s.pane = 1
	case "up", "k":
		if s.pane == 0 && s.projectCursor > 0 {
			s.projectCursor--
			loadMemoryEntries(s)
		} else if s.pane == 1 && s.memoryCursor > 0 {
			s.memoryCursor--
		}
	case "down", "j":
		if s.pane == 0 && s.projectCursor < len(s.projects)-1 {
			s.projectCursor++
			loadMemoryEntries(s)
		} else if s.pane == 1 && s.memoryCursor < len(s.entries)-1 {
			s.memoryCursor++
		}
	case "enter":
		if s.pane == 0 {
			s.pane = 1
		}
		if len(s.entries) > 0 {
			entry, err := readMemory(s.root, s.projects[s.projectCursor].Name, s.entries[s.memoryCursor].Filename)
			s.err = err
			if err == nil {
				s.mode = memoryRead
				s.reader = viewport.New(viewport.WithWidth(memoryContentWidth(m)), viewport.WithHeight(max(3, s.height-10)))
				s.reader.SoftWrap = true
				s.reader.SetContent(entry.Content)
			}
		}
	case "n":
		s.mode = memoryNewProject
		s.err = nil
		s.notice = ""
		project := ""
		if len(s.projects) > 0 {
			project = s.projects[s.projectCursor].Name
		}
		return m, memoryInput(s, "Project name (existing or new)", project)
	case "/":
		s.mode = memorySearch
		return m, memoryInput(s, "Filter this project's full text", s.filter)
	case "d":
		if len(s.entries) > 0 {
			s.mode = memoryDelete
		}
	}
	return m, nil
}

func memoryContentWidth(m model) int {
	if m.width == 0 {
		return 90
	}
	return max(20, min(90, m.width-12))
}

func memoryView(m model) string {
	s := m.memory
	content := m.styles.title.Render("Project Memory") + "\n"
	hint := "j/k: move • tab/h/l: pane • enter: read • n: new • /: filter • d: delete • esc: menu • q: quit"
	switch s.mode {
	case memoryNewProject, memoryNewTitle:
		label := "Project name (edit to create another project)"
		if s.mode == memoryNewTitle {
			label = "Memory title"
		}
		content += label + "\n\n" + s.input.View()
		hint = "enter: next • esc: cancel"
	case memoryNewBody:
		content += s.newProject + " / " + s.newTitle + "\n\n" + s.body.View()
		hint = "enter: newline • ctrl+s: save • esc: cancel"
	case memoryRead:
		content += s.reader.View()
		hint = "j/k/up/down/pgup/pgdown: scroll • esc/enter: back • q: quit"
	default:
		if len(s.projects) == 0 {
			content += "\nNo project memories yet. Press n to create one.\n"
		} else {
			width := memoryContentWidth(m)
			leftWidth := max(10, width/3)
			rightWidth := max(10, width-leftWidth-3)
			left := "Projects"
			right := "Memories"
			if s.pane == 0 {
				left = m.styles.selected.Render(left)
			} else {
				right = m.styles.selected.Render(right)
			}
			rows := max(2, (s.height-15)/2)
			for i := max(0, s.projectCursor-rows+1); i < len(s.projects) && i < max(0, s.projectCursor-rows+1)+rows; i++ {
				p := s.projects[i]
				line := fmt.Sprintf("  %s (%d)", p.Name, p.Count)
				if i == s.projectCursor {
					line = m.styles.selected.Render("▸ " + fmt.Sprintf("%s (%d)", p.Name, p.Count))
				}
				left += "\n" + ansi.Truncate(line, leftWidth, "…")
			}
			if len(s.entries) == 0 {
				if s.filter != "" {
					right += "\nNo matching memories. Press / to change the filter."
				} else {
					right += "\nNo memories in this project. Press n to create one."
				}
			}
			for i := max(0, s.memoryCursor-rows+1); i < len(s.entries) && i < max(0, s.memoryCursor-rows+1)+rows; i++ {
				entry := s.entries[i]
				line := "  " + entry.Title
				if i == s.memoryCursor {
					line = m.styles.selected.Render("▸ " + entry.Title)
				}
				right += "\n" + ansi.Truncate(line, rightWidth, "…")
			}
			if len(s.entries) > 0 {
				entry := s.entries[s.memoryCursor]
				right += "\n\n" + m.styles.subtle.Render(ansi.Truncate(entry.Filename, rightWidth, "…")+"\n"+entry.Date) + "\n\n" + lipgloss.NewStyle().Width(rightWidth).MaxHeight(3).Render(entry.Preview)
			}
			content += lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(leftWidth).MaxWidth(leftWidth).Render(left), " │ ", lipgloss.NewStyle().Width(rightWidth).MaxWidth(rightWidth).Render(right))
		}
		if s.filter != "" {
			content += "\nFilter: " + s.filter
		}
		if s.mode == memorySearch {
			content += "\n/ " + s.input.View()
			hint = "type: filter current project • enter: keep • esc: clear"
		}
		if s.mode == memoryDelete {
			content += "\n" + m.styles.warn.Render("Delete "+s.entries[s.memoryCursor].Filename+"? y/n")
			hint = "y: delete • n/esc: cancel"
		}
	}
	if s.err != nil {
		content += "\n" + m.styles.errStyle.Render(s.err.Error())
	}
	if s.notice != "" {
		content += "\n" + m.styles.success.Render(s.notice)
	}
	return m.borderBox(content + "\n\n" + m.styles.subtle.Render(hint))
}
