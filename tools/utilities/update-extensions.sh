#!/usr/bin/env bash
# Run after adding or removing extensions to update the lists in the repo.
# Then commit and push as usual.

DOTFILES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ANTIGRAVITY_CLI="$HOME/.antigravity/antigravity/bin/antigravity"
CURSOR_CLI="/Applications/Cursor.app/Contents/Resources/app/bin/cursor"

if command -v "$ANTIGRAVITY_CLI" &>/dev/null; then
  "$ANTIGRAVITY_CLI" --list-extensions > "$DOTFILES_DIR/vscode/extensions-antigravity.txt"
  echo "updated extensions-antigravity.txt"
fi

if command -v "$CURSOR_CLI" &>/dev/null; then
  "$CURSOR_CLI" --list-extensions > "$DOTFILES_DIR/vscode/extensions-cursor.txt"
  echo "updated extensions-cursor.txt"
fi

echo ""
echo "Now commit and push:"
echo "  cd ~/dotfiles && git add -A && git commit -m 'update extensions' && git push"
