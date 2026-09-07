#!/bin/sh
# Build xuz and install it to ~/.local/bin (creating it if needed).
set -eu

cd "$(dirname "$0")"

BIN="$(pwd)/dist/xuz"
DEST_DIR="${HOME}/.local/bin"
DEST="${DEST_DIR}/xuz"

go build -o "$BIN" ./cmd/xuz
mkdir -p "$DEST_DIR"
install -m 0755 "$BIN" "$DEST"
echo "installed $DEST"

if [ ! -f "${HOME}/.xuz/config.yml" ]; then
  "$DEST" init
fi
echo "run 'xuz' to start."
echo "for shell completions, add to your shell rc (after any alias definitions):"
echo "  eval \"\$(xuz completions bash)\"   # or 'zsh' / 'fish'"
