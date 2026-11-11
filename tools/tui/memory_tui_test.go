package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func memoryTestKey(m model, key string) model {
	code := rune(-1)
	switch key {
	case "enter":
		code = tea.KeyEnter
	case "esc":
		code = tea.KeyEscape
	case "tab":
		code = tea.KeyTab
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: code, Text: key})
	return next.(model)
}

func memoryTestModel(t *testing.T) model {
	t.Helper()
	return model{screen: screenMemory, styles: newStyles(), width: 100, memory: memoryState{root: t.TempDir(), height: 30}}
}

func TestMemoryCreateFirstProject(t *testing.T) {
	m := memoryTestModel(t)
	if !strings.Contains(memoryView(m), "No project memories yet. Press n") {
		t.Fatal("missing empty-state guidance")
	}
	m = memoryTestKey(m, "n")
	if m.memory.mode != memoryNewProject {
		t.Fatal("new should prompt for project")
	}
	m.memory.input.SetValue("demo")
	m = memoryTestKey(m, "enter")
	if m.memory.mode != memoryNewTitle {
		t.Fatal("project should advance to title")
	}
	m.memory.input.SetValue("Launch notes")
	m = memoryTestKey(m, "enter")
	if m.memory.mode != memoryNewBody {
		t.Fatal("title should advance to body")
	}
	m.memory.body.SetValue("Real context\nSecond line")
	next, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = next.(model)
	if m.memory.err != nil {
		t.Fatal(m.memory.err)
	}
	if m.memory.mode != memoryBrowse || len(m.memory.projects) != 1 || len(m.memory.entries) != 1 {
		t.Fatalf("save did not refresh browser: %+v", m.memory)
	}
	if m.memory.entries[0].Title != "Launch notes" {
		t.Fatal("saved title missing")
	}
}

func TestMemoryReadFilterAndDelete(t *testing.T) {
	m := memoryTestModel(t)
	_, err := saveMemory(m.memory.root, "demo", "Refactor notes", strings.Repeat("refactor context\n", 100), nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	refreshMemory(&m.memory, "")
	m = memoryTestKey(m, "tab")
	if m.memory.pane != 1 {
		t.Fatal("tab must swap panes")
	}
	m = memoryTestKey(m, "enter")
	if m.memory.mode != memoryRead {
		t.Fatal("enter must open full memory")
	}
	m = memoryTestKey(m, "j")
	if m.memory.reader.YOffset() == 0 {
		t.Fatal("reader must scroll")
	}
	m = memoryTestKey(m, "esc")
	if m.screen != screenMemory || m.memory.mode != memoryBrowse {
		t.Fatal("reader esc must return to browser")
	}
	m = memoryTestKey(m, "/")
	m.memory.input.SetValue("absent")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(model)
	if len(m.memory.entries) != 0 {
		t.Fatal("filter should hide non-matches")
	}
	m = memoryTestKey(m, "esc")
	if len(m.memory.entries) != 1 || m.memory.filter != "" {
		t.Fatal("filter esc must clear filter")
	}
	m = memoryTestKey(m, "d")
	m = memoryTestKey(m, "n")
	if len(m.memory.entries) != 1 {
		t.Fatal("delete cancellation must preserve file")
	}
	m = memoryTestKey(m, "d")
	m = memoryTestKey(m, "y")
	if m.memory.err != nil || len(m.memory.entries) != 0 {
		t.Fatal("confirmed deletion must refresh", m.memory.err)
	}
	m = memoryTestKey(m, "esc")
	if m.screen != screenMenu {
		t.Fatal("browser esc must return to menu")
	}
}

func TestMemoryPromptEscapeAndQuit(t *testing.T) {
	m := memoryTestModel(t)
	m = memoryTestKey(m, "n")
	m = memoryTestKey(m, "q")
	if m.memory.input.Value() != "q" {
		t.Fatal("q must type in an input")
	}
	m = memoryTestKey(m, "esc")
	if m.screen != screenMemory || m.memory.mode != memoryBrowse {
		t.Fatal("esc must cancel prompt")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: -1, Text: "q"})
	if cmd == nil {
		t.Fatal("q must quit browsing")
	}
}

func TestMemoryReaderResizeAndInputPaste(t *testing.T) {
	m := memoryTestModel(t)
	m = memoryTestKey(m, "n")
	next, _ := m.Update(tea.PasteMsg{Content: "Saturn – café 🚀"})
	m = next.(model)
	if m.memory.input.Value() != "Saturn – café 🚀" {
		t.Fatal("input must preserve Unicode paste", m.memory.input.Value())
	}
	m = memoryTestKey(m, "esc")
	_, err := saveMemory(m.memory.root, "demo", "Notes", strings.Repeat("context ", 100), nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	refreshMemory(&m.memory, "")
	m = memoryTestKey(m, "enter")
	next, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m = next.(model)
	if m.memory.reader.Width() != 58 || m.memory.reader.Height() != 10 || !m.memory.reader.SoftWrap {
		t.Fatal("reader must resize and wrap")
	}
}
