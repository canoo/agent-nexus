package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type versionCheckMsg struct {
	latest    string
	updateURL string
	err       error
}

type updateDoneMsg struct {
	err error
}

// --- update ---

const repoAPI = "https://api.github.com/repos/canoo/agent-nexus/releases/latest"

func checkLatestVersion() tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(repoAPI)
		if err != nil {
			return versionCheckMsg{err: err}
		}
		defer resp.Body.Close()
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return versionCheckMsg{err: err}
		}
		latest := strings.TrimPrefix(release.TagName, "v")
		return versionCheckMsg{
			latest:    latest,
			updateURL: "https://github.com/canoo/agent-nexus/releases/tag/" + release.TagName,
		}
	}
}

var validTag = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)

func runSelfUpdate(tag string) tea.Cmd {
	return func() tea.Msg {
		if !validTag.MatchString(tag) {
			return updateDoneMsg{err: fmt.Errorf("invalid version tag: %q", tag)}
		}
		// tag is passed as $1 so it is never interpolated into the script text.
		cmd := exec.Command("bash", "-c", selfUpdateScript(), "bash", tag)
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) > 0 {
			err = fmt.Errorf("%w: %s", err, string(out))
		}
		return updateDoneMsg{err: err}
	}
}

// selfUpdateScript downloads the release's install.sh and checksums.txt,
// verifies install.sh against its checksums.txt entry (added via
// checksum.extra_files in .goreleaser.yml), then executes it.
func selfUpdateScript() string {
	return `
set -e
TAG="$1"
BASE="https://github.com/canoo/agent-nexus/releases/download/v${TAG}"
SCRIPT=$(mktemp)
SUMS=$(mktemp)
trap 'rm -f "$SCRIPT" "$SUMS"' EXIT

curl -sSL "${BASE}/install.sh"      -o "$SCRIPT"
curl -sSL "${BASE}/checksums.txt"   -o "$SUMS"

EXPECTED=$(awk '$2 == "install.sh" {print $1}' "$SUMS")
if [ -z "$EXPECTED" ]; then
  echo "checksum entry for install.sh not found" >&2; exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  echo "$EXPECTED  $SCRIPT" | shasum -a 256 --check --status
elif command -v sha256sum >/dev/null 2>&1; then
  echo "$EXPECTED  $SCRIPT" | sha256sum --check --status
else
  echo "no sha256 tool available" >&2; exit 1
fi

bash "$SCRIPT"
`
}

func updateUpdateScreen(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case versionCheckMsg:
		m.running = false
		if msg.err != nil {
			m.err = msg.err
		} else if msg.latest != "" && version != "dev" {
			m.latestVersion = msg.latest
			m.updateURL = msg.updateURL
			if msg.latest == version {
				m.updateNotice = ""
			} else {
				m.updateNotice = "Update available: v" + msg.latest
			}
		}
		return m, nil
	case updateDoneMsg:
		m.running = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.output = "Updated successfully! Restart nexus to use the new version."
			m.latestVersion = ""
			m.updateNotice = ""
		}
		return m, nil
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenMenu
			return m, nil
		case "u":
			if m.latestVersion != "" && m.latestVersion != version && !m.running {
				m.running = true
				m.output = ""
				m.err = nil
				return m, tea.Batch(m.spinner.Tick, runSelfUpdate(m.latestVersion))
			}
		}
	}
	return m, nil
}

func updateScreenView(m model) string {
	s := m.styles.title.Render("⬆ Update NEXUS") + "\n\n"
	s += m.styles.subtle.Render("Current version: v"+version) + "\n"

	if m.running {
		s += m.spinner.View() + " Checking...\n"
	} else if m.err != nil {
		s += m.styles.errStyle.Render("✗ "+m.err.Error()) + "\n"
	} else if m.output != "" {
		s += m.styles.success.Render("✓ "+m.output) + "\n"
	} else if m.latestVersion == "" || m.latestVersion == version {
		s += m.styles.success.Render("✓ You're on the latest version") + "\n"
	} else {
		s += m.styles.warn.Render("⬆ New version available: v"+m.latestVersion) + "\n"
		s += "\n" + m.styles.subtle.Render("Press u to update • esc: back")
		return m.borderBox(s)
	}

	s += "\n" + m.styles.subtle.Render("esc: back")
	return m.borderBox(s)
}
