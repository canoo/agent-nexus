package main

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestIntegratedMenuScreens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		label string
		want  screen
	}{
		{"Companion Tool Activity", screenCompanionActivity},
		{"Uninstall NEXUS", screenUninstall},
		{"Project Memory", screenMemory},
	} {
		t.Run(tc.label, func(t *testing.T) {
			m := initialModel()
			found := false
			for i, label := range menuItems {
				if label == tc.label {
					m.cursor = i
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing menu entry %q", tc.label)
			}
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if got := next.(model).screen; got != tc.want {
				t.Fatalf("%s opened screen %d, want %d", tc.label, got, tc.want)
			}
		})
	}
}
