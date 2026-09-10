#!/bin/bash
# theme-sync.sh — Sync the current Omarchy theme into xuz config.yml.
#
# Usage: theme-sync.sh [THEME_SLUG]
#   Without an argument, reads the current Omarchy theme from
#   ~/.local/state/omarchy/current/theme.name.
#   With an argument, uses that slug directly.
#
# When run as a hook (theme-set.d), $1 is already the slug.
#
# The script maps Omarchy colors.toml fields to xuz Theme fields:
#   background     → bg
#   foreground     → fg
#   accent         → accent
#   green          → selected
#   lighter_background → cursor_bg
#   foreground     → cursor_fg
#   selection      → border
#   accent         → border_active
#   accent         → title
#   dark_foreground → muted
#   accent         → status
#   dark_foreground → help
#   red            → error
#   green          → success

set -euo pipefail

XUZ_CONFIG="${XUZ_CONFIG:-$HOME/.xuz/config.yml}"

# --- helpers ----------------------------------------------------------

toml_get() {
  # toml_get <file> <key>
  # Returns the value (stripped of quotes) or empty string.
  local file="$1" key="$2"
  grep -E "^${key}\s*=" "$file" 2>/dev/null \
    | head -1 \
    | sed 's/^[^=]*=\s*//' \
    | sed 's/^"\(.*\)"$/\1/' \
    | sed "s/^'\(.*\)'$/\1/" \
    || true
}

ensure_xuz_config() {
  if [[ ! -f "$XUZ_CONFIG" ]]; then
    echo "xuz config not found at $XUZ_CONFIG — run 'xuz init' first" >&2
    return 1
  fi
}

# --- find colors.toml -----------------------------------------------

find_colors_toml() {
  local slug="$1"
  # 1. User themes (highest priority)
  if [[ -f "$HOME/.config/omarchy/themes/${slug}/colors.toml" ]]; then
    echo "$HOME/.config/omarchy/themes/${slug}/colors.toml"
    return 0
  fi
  # 2. Stock themes
  if [[ -f "/usr/share/omarchy/themes/${slug}/colors.toml" ]]; then
    echo "/usr/share/omarchy/themes/${slug}/colors.toml"
    return 0
  fi
  return 1
}

# --- mapping ----------------------------------------------------------

map_theme() {
  local colors_file="$1"
  local slug="$2"

  local bg fg accent selected cursor_bg cursor_fg border border_active title muted status help error success
  bg=$(toml_get "$colors_file" "background")
  fg=$(toml_get "$colors_file" "foreground")
  accent=$(toml_get "$colors_file" "accent")
  selected=$(toml_get "$colors_file" "green")
  cursor_bg=$(toml_get "$colors_file" "lighter_background")
  cursor_fg="$fg"
  border=$(toml_get "$colors_file" "selection")
  border_active="$accent"
  title="$accent"
  muted=$(toml_get "$colors_file" "dark_foreground")
  status="$accent"
  help="$muted"
  error=$(toml_get "$colors_file" "red")
  success="$selected"

  # Only emit the theme block if we got at least some colors.
  if [[ -z "$bg" && -z "$fg" ]]; then
    echo "no colors found in $colors_file" >&2
    return 1
  fi

  # Write a themes: block into the config. We insert it before the aliases:
  # key so the canonical format is preserved (themes comes after last_used
  # but before aliases, per config.go's Save writer).

  local theme_block=""
  theme_block+="# Auto-generated from Omarchy theme '$slug'. Do not edit manually.\n"
  theme_block+="themes:\n"
  theme_block+="  ${slug}:\n"

  local fields=(
    "bg:${bg}"
    "fg:${fg}"
    "accent:${accent}"
    "selected:${selected}"
    "cursor_bg:${cursor_bg}"
    "cursor_fg:${cursor_fg}"
    "border:${border}"
    "border_active:${border_active}"
    "title:${title}"
    "muted:${muted}"
    "status:${status}"
    "help:${help}"
    "error:${error}"
    "success:${success}"
  )

  for entry in "${fields[@]}"; do
    local field="${entry%%:*}"
    local value="${entry#*:}"
    # Only include fields that have a value
    if [[ -n "$value" ]]; then
      theme_block+="    ${field}: \"${value}\"\n"
    fi
  done

  echo "$theme_block"
}

# --- main -------------------------------------------------------------

main() {
  local slug=""

  if [[ $# -ge 1 ]]; then
    slug="$1"
  else
    # Try to read from Omarchy state.
    local theme_path="$HOME/.local/state/omarchy/current/theme.name"
    if [[ -f "$theme_path" ]]; then
      slug=$(tr '[:upper:]' '[:lower:]' < "$theme_path" | tr -d '[:space:]')
    fi
  fi

  if [[ -z "$slug" ]]; then
    echo "could not determine current Omarchy theme" >&2
    return 1
  fi

  ensure_xuz_config || return 1

  local colors_file
  if ! colors_file=$(find_colors_toml "$slug"); then
    echo "no colors.toml found for theme '$slug'" >&2
    return 1
  fi

  local theme_block
  if ! theme_block=$(map_theme "$colors_file" "$slug"); then
    return 1
  fi

  # Strategy: if the config already has a "themes:" block, replace it in
  # place. Otherwise, insert before "aliases:".
  if grep -q '^themes:' "$XUZ_CONFIG"; then
    # Replace the existing themes block. We strip everything from "themes:"
    # down to (but not including) the next top-level key (a line not starting
    # with whitespace, e.g. "aliases:").
    local tmp
    tmp=$(awk -v block="$theme_block" '
      BEGIN { skip = 0; done_replace = 0 }
      /^themes:/ && !done_replace {
        skip = 1
        done_replace = 1
        printf "%s", block
        next
      }
      skip && /^[a-zA-Z]/ { skip = 0 }
      skip { next }
      { print }
    ' "$XUZ_CONFIG")
    echo "$tmp" > "$XUZ_CONFIG"
  else
    # Insert before aliases: so the canonical order is preserved.
    local tmp
    tmp=$(awk -v block="$theme_block" '
      /^aliases:/ { printf "%s", block }
      { print }
    ' "$XUZ_CONFIG")
    echo "$tmp" > "$XUZ_CONFIG"
  fi

  # Set the active theme to match.
  if grep -q '^theme:' "$XUZ_CONFIG"; then
    local tmp
    tmp=$(sed "s/^theme:.*/theme: ${slug}/" "$XUZ_CONFIG")
    echo "$tmp" > "$XUZ_CONFIG"
  else
    local tmp
    tmp=$(sed "1s/^/theme: ${slug}\n/" "$XUZ_CONFIG")
    echo "$tmp" > "$XUZ_CONFIG"
  fi

  echo "synced xuz theme from Omarchy '${slug}' → $XUZ_CONFIG"
}

main "$@"
