# xuz

A small Go TUI for launching configured commands. One column per option group:
pick a model, a context size, whatever your config defines — then hit Enter.

## Two modes

**Short mode** (the default, `xuz`) has no full screen. It shows the
alias list — four matches, with the `filter` line at the bottom of the
list — then, once an alias is picked, one option column at a time (its
name plus up to four options).
Enter always launches with the preselected options; the shortcut tooltip
line is always visible:

```
alias:                                                                     »
▸ llama
filter ll█

󱊷  exit · 󱁐 select · ↑↓←→ move · ↵ launch · / switch

model:                                                                     «  »
▸ qwen-3.8-27B  /home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf
  qwen-3.6-35B-A3B localweights_Qwen3.6-35B-A3B-...

󱊷  exit · 󱁐 select · ↑↓←→ move · ↵ launch · / switch
```

The `«`/`»` signs in the header point at the columns next to the current one
(the alias list sits left of every option column; the remaining option
groups sit to its right). In the alias list, `right`/`tab` step into the
selected alias's first option column (the ▸ needle moves to it),
`left`/`esc` from a column step back to the list, keeping the filter you
typed (empty if you typed nothing).

**Full mode** (`xuz -f`, `xuz --full`) is the full-screen multi-column picker
with all the options:

```
┌─ ALIAS ─────────┬─ MODEL ───────────────────────────────────────┬─ CONTEXT ─────────────┐
│ ▸ llama          │ ▸ qwen-3.8-27B  /home/models/Unsloth_mtp-...  │   64k   65536          │
│   whisper        │   qwen-3.6-35B-A3B localweights_Qwen3.6-35B-..│   256k  262144        │
└─────────────────┴───────────────────────────────────────────────┴────────────────────────┘
$ exec llama-server -m "/home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf" -c "65536"
󱊷  exit · ↑↓←→ move · 󱁐 select · ↵ run · f filter · t theme
```

From the short mode, `/` switches to the full mode, and from the
full mode it comes back, keeping the current alias, cursors,
selections and the typed alias filter. The full mode's alias column always
starts plain (like a values column) — the typed alias filter carries over
inert and only the `f` shortcut enters the filter mode there; an active
filter of an option column does not carry over, as it is not an alias
filter. After a run, xuz exits with the command's status — the
picker never reopens (see below).

## Quick start

```sh
go build -o dist/xuz ./cmd/xuz
dist/xuz init        # writes ~/.xuz/config.yml (sample)
dist/xuz             # open the picker (short mode)
dist/xuz -f          # open the full-screen picker
```

Or install: `make install` → `~/.local/bin/xuz`.

## How it works

`xuz` reads `~/.xuz/config.yml` (or `$XUZ_CONFIG`, or `--config PATH`). Each
alias defines ordered option groups; each group is a list of options — a key
with either a scalar long text (the old form), an object of `icon` +
`long_text`, or nothing at all (the no-pair form, where the long text is the
key itself). The TUI shows:

1. one column with all aliases,
2. one column per option group of the selected alias, in config order.

When you press Enter, the command of the selected alias runs through `sh -c`
with one environment variable per option group: **the group name,
uppercased**, set to the **selected option's long text**:

| group in config | env var | long text |
|---|---|---|
| `model` | `$MODEL` | `/home/models/....gguf` (the long text, not the key) |
| `context` | `$CONTEXT` | `65536` |

So `command: exec llama-server -m "$MODEL" -c "$CONTEXT"` is all you need.
(The footer shows the command with variables substituted, live; when the
terminal is too narrow for a single line it wraps onto extra footer lines
instead of being cut — the columns shrink to make room.)

After the command exits, xuz exits with the command's status — the picker
never reopens, whatever the outcome (success or failure). To tweak one group
and relaunch, run `xuz` (or `xuz <alias>`) again: your last selections are
preselected from the history (a group's `!default` tag, if any, wins over
the history — see Preselection).

### Preselection

When you open an alias, the initial selection in each column is resolved by:

1. a `!default` tag on an option's long text in the group,
2. the most recent launch of that alias in `~/.xuz/history`,
3. `last_used` (a global list, or a per-alias map — the map wins),
4. the first option in the group.

The selected option is marked `▸`, in the theme's `selected` color. When
the selection was preselected from the history rather than from a `!default`
tag, the `▸` is rendered in the theme's `muted` color instead (a themed
color, overridable via custom themes, cursor-background aware) to show it is
your last-run option rather than a default.

### History

Every Enter writes one line to `~/.xuz/history`:

```
2026-02-07T12:34:56+00:00  llama  model=qwen-3.8-27B  context=64k
```

The file is trimmed to the last `remember_last` entries (default 10; an alias
may set its own `remember_last:`).

## Keys

The shortcut line always shows the keys that apply to the current context.

### Short mode

| key | action |
|---|---|
| typing | fuzzy-filter the aliases (fzf-style subsequence, e.g. `ll` → `llama`); `backspace` edits, `esc` clears the filter |
| `up` / `down` | move through the matches (or through the four options of a column) |
| `enter` | launch the command with the preselected options — from the alias list or from any column |
| `space` / `right` / `tab` | in the alias list: open the selected alias's first option column, selecting the alias under the cursor (the ▸ needle moves to it; the `»` sign points there); in a column: `space` selects the option under the cursor and the alias of that column. With a filter that matches nothing, `space`/`right`/`tab` do nothing — there is no alias to open |
| `left` / `right` (`shift+tab` / `tab`) | previous / next option column; from the first column, `left` goes back to the alias filter (the `«` sign points there), keeping the filter you typed |
| `/` | switch to the full mode (selections and the typed filter are kept) |
| `esc` | clear the filter (alias filter) or go back to the alias filter (columns, keeping the filter); quit from the alias filter with an empty filter |
| `q` / `ctrl-c` | quit — if the config has unsaved changes, xuz asks first. In the alias filter every letter (including `q`) is a filter character; only `ctrl-c` quits |

### Full mode

| key | action |
|---|---|
| `up` / `down` (`k`/`j`) | move the cursor; in the alias column this switches alias |
| `left` / `right` | previous / next column |
| `tab` / `shift+tab` | next / previous column. Columns are sized to their content and share the leftover width; when they don't all fit, a thin sliver of the next column is drawn at the right edge (with a `»` hint in the shortcut line) and `right`/`tab` scroll the viewport so the active column is always fully visible |
| `space` | select the item under the cursor (each column keeps its own selection); in an option column this also selects the alias of that column (the ▸ needle follows it, green) |
| `enter` | run the command — works from **any** column |
| `f` | start the filter; typing filters the options in the current column (fzf-style subsequence, e.g. `q38` → `qwen-3.8-27B`). `esc` exits the filter, `enter` runs, `backspace` edits |
| `n` | add a new option to the current column (`key: long text` or `key longtext`) |
| `d` | delete the option under the cursor (the last one in a group is protected) |
| `ctrl+space` | make the option under the cursor the default |
| `c` | copy the resolved command to the clipboard (OSC 52, or pbcopy/xclip/… if available) |
| `ctrl+s` | save the config to `~/.xuz/config.yml` (or `$XUZ_CONFIG` / `--config PATH`) |
| `b` | toggle **background mode** (runs nothing). While armed, `enter` launches the *user command* in the background — suffixed with ` &` in a new session, so it keeps running after xuz exits — and its log lands next to the history file, e.g. `~/.xuz/llama-20260207-123456.log`. Press `b` again to disarm |
| mouse (left click) | click a column to focus it; click an option row to select it (same as `space`); click the alias column to switch alias. Ignored while filtering or at a prompt |
| `t` | cycle themes (builtins, then your custom ones) |
| `?` | show the config/theme info line |
| `/` | short mode (selections and the typed alias filter are kept, inert in the full mode) |
| `esc` | quit — from the alias column or any option column (the `󱊷  exit` item, first in the shortcut line); while filtering it exits the filter instead |
| `q` / `ctrl-c` | quit — if the config has unsaved changes, xuz asks first (`y` saves and quits, `n` quits without saving) |

Additions and deletions change the config in memory; nothing is written until
you press `ctrl+s` or answer `y` at the quit prompt.

## Config reference

```yaml
theme: default            # default | dracula | gruvbox | monokai | catppuccin
                          # solarized-dark | solarized-light | light, or a name
                          # from themes: below
remember_last: 10         # history depth (default 10)

last_used:                # preselection hints
  - qwen-3.6-35B-A3B      # global list of option keys
# last_used:              # or per-alias map (beats the global list)
#   llama: [qwen-3.6-35B-A3B]

aliases:
  llama:
    icon: 🦙                  # optional: a literal icon glyph (an emoji or a
                              # nerd-font glyph) shown at the left of the
                              # alias name; !colorXXXXXX colors it
                              # (icon: !colorFF5555 ⚙)
    options:
      model:
        - qwen-3.8-27B: /home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf
        - qwen-3.6-35B-A3B: !default localweights_Qwen3.6-35B-A3B-MTP-IMAT-IQ4_XS-Q8nextn.gguf
        # the object form — icon + long_text (long_text may be omitted,
        # then it is the key text):
        # - qwen-3.6-35B-A3B:
        #     icon: 🦙
        #     long_text: !default /path/to/model.gguf
        # and the no-pair form — long text = the key text, no icon:
        # - dev:
      context:
        - 64k: 65536
        - 256k: 262144        # the space after "-" is required in YAML
      command: exec llama-server -m "$MODEL" -c "$CONTEXT"
    remember_last: 20         # optional per-alias override

  whisper:
    options:
      model:
        - large-v3: /home/models/whisper-large-v3.pt
      command: whisper "$MODEL"

# Optional extra env vars: name -> group (merged on top of the by-name vars)
#   vars:
#     CTX: context

# Custom themes (merged over "default", or over a builtin of the same name):
# themes:
#   mytheme:
#     bg: "#1a1b26"
#     fg: "#c0caf5"
#     accent: "#7aa2f7"
#     selected: "#9ece6a"
#     cursor_bg: "#24283b"
#     cursor_fg: "#c0caf5"
#     border: "#3b4261"
#     border_active: "#7aa2f7"
#     title: "#7aa2f7"
#     muted: "#565f89"
#     status: "#7aa2f7"
#     help: "#565f89"
#     error: "#f7768e"
#     success: "#9ece6a"
```

Notes:

- Option groups render in the order they appear in the file.
- `command` may live inside `options:` (as above) or directly under the alias.
- `theme`, `remember_last`, and `last_used` may also sit directly under
  `aliases:` (next to the alias names, as in
  `aliases: { llama: …, last_used: [qwen-3.6-35B-A3B], remember_last: 10 }`);
  they then mean the same as at the top level, and the top level wins when
  both are present.
- `last_used` keys may be bound to a group (the key is looked up in any
  group that contains it).
- An option is a key with either a scalar long text
  (`- 64k: 65536`, the old form), an object of `icon` + `long_text`
  (`- prod: {icon: ⚠, long_text: production}`), or nothing at all
  (`- dev:`): the no-pair form, where the long text is the key text.
  `long_text` may be omitted from the object (then it is the key text).
- The `!default` tag on an option's long text preselects it (it wins over the
  history preselection; the first option is preselected when no option is
  tagged and there is no history or `last_used` match). In the object form
  the tag rides on the `long_text` entry.
- The `!colorXXXXXX` tag on an option's long text colors the option's key in
  the given hex color in its column; the long text, when shown next to the
  key, keeps the muted value color (`- stag: !colorFFD814 staging` shows
  `stag` in yellow and `staging` muted). When the long text equals the key it
  is not shown, so `- prod: !colorD30000 prod` shows `prod` in red. A
  leading `#` is accepted (`!color#D30000`). The tags can be combined into a
  compound tag — `!tag1+tag2+...+tagN`, the parts in any order:
  `!default+colorXXXXXX` preselects the option and colors its key
  (`!colorXXXXXX+default` is equivalent; saving keeps the order written).
  A malformed compound tag (an unknown part, an invalid color) is ignored
  with a warning that names the line of the file. The tag is display-only:
  the command still receives the plain long text.
- Unknown keys produce a warning on stderr, not an error.
- Color values accept `#rrggbb`, the 16 ANSI names, 256-color indexes, or
  `default` (terminal default).
- An alias may set `icon:` to decorate its name in the alias column, and an
  option may set `icon:` inside its object to decorate its key in the option
  column. Icons are literal text glyphs — an emoji or a nerd-font glyph,
  rendered as-is at the left of the name/key, ahead of the `▸` marker (both
  in the short and the full mode). No image files and no cache are involved.
  A `!colorXXXXXX` tag on the icon colors the glyph (display-only,
  cursor-aware like the option colors); several `!color` parts joined by
  `+` are allowed (the last one wins, with a warning), and no other tag
  (such as `!default`) applies to icons — a tag carrying a non-color part
  is malformed and is ignored with a warning. An invalid color tag produces
  the same warning as on option values.
- Every column (the alias column and each option column) has a leading icon
  slot, as wide in cells as the widest icon set on that column (color tags
  do not affect the width), and 0 when the column has no icons. Every row is
  `[slot][▸ / two spaces][name]` (+ ` long_text` when it is shown), so all
  names/keys in a column start at the same cell.

## CLI

```
xuz [alias]                  open the picker (start on alias if given)
xuz -f [alias]               same, in the full-screen mode (default: short mode)
xuz run <alias>              same
xuz list | ls [--names]      list aliases and options (--names: the alias
                             names only, one per line — used by the shell
                             completion scripts)
xuz themes                   list themes (builtins + custom, marks active)
xuz theme-sync               sync current Omarchy theme into xuz config
xuz init [--config PATH]     write the sample config
xuz show <alias>             print resolved selections + substituted command
xuz completions [shell]      print a shell completion script (bash | zsh |
                             fish; the shell is guessed from $SHELL when
                             omitted)
xuz --version

-f, --full     full-screen mode (default is the short mode)
--config PATH  config file (default $XUZ_CONFIG or ~/.xuz/config.yml)
--theme NAME   override the theme for this run
--dry-run      print the resolved command instead of running it (no history)
               without a TTY it resolves non-interactively from
               history > last_used > defaults and prints just the command
```

## Shell completions

`xuz completions <shell>` prints a completion script for `bash`, `zsh` or
`fish` (the shell is guessed from `$SHELL` when the argument is omitted).
It completes the flags, the subcommands and — from your config, on every
keystroke — the alias names, both as the first argument and after `run`
and `show`:

```
xuz tf-pl<TAB>    →  xuz tf-plan
xuz ll<TAB>       →  xuz llama
xuz run tf-a<TAB> →  xuz run tf-apply
```

Add one line to your shell rc:

```sh
# ~/.bashrc
eval "$(xuz completions bash)"
# ~/.zshrc
eval "$(xuz completions zsh)"
# ~/.config/fish/config.fish
xuz completions fish | source
```

The script is generated for the name the binary is invoked under (the base
name of `argv[0]`): `xuz completions bash` registers the completions for
`xuz` (so `./xuz` and a full path complete the same way). A `--config
PATH` given while generating the completions is passed along to the
binary; otherwise the usual `$XUZ_CONFIG` or `~/.xuz/config.yml` is read.
In bash and zsh, a shell alias that runs the binary gets the completions
too — define such aliases before the line above.

The alias names are read with `xuz ls --names` at completion time, so new
aliases in the config are completed without regenerating the script.

## Omarchy theme syncing

When running on [Omarchy](https://omarchy.org/), xuz can automatically adopt
the active desktop theme so its colors always match your Hyprland theme.

**Manual sync** — run once after installing xuz:

```sh
xuz theme-sync
```

This reads `$HOME/.local/state/omarchy/current/theme.name`, finds the theme's
`colors.toml` (from `~/.config/omarchy/themes/<slug>/` or
`/usr/share/omarchy/themes/<slug>/`), maps the color fields to a xuz custom
theme, sets it as the active theme, and saves the config.

**Automatic sync** — install a hook so it runs every time you change your
Omarchy theme:

```sh
mkdir -p ~/.config/omarchy/hooks/theme-set.d/
cp $(which xuz)/../scripts/theme-sync.sh ~/.config/omarchy/hooks/theme-set.d/
chmod +x ~/.config/omarchy/hooks/theme-set.d/sync-xuz.sh
```

The hook script receives the new theme slug as `$1` and runs `theme-sync.sh`
to update `~/.xuz/config.yml` in place. It writes a `themes:` block with all
mapped colors and updates the `theme:` line to the new slug.

**Field mapping** (Omarchy `colors.toml` → xuz theme field):

| Omarchy field            | xuz field        |
|--------------------------|------------------|
| `background`             | `bg`             |
| `foreground`             | `fg`             |
| `accent`                 | `accent`         |
| `green`                  | `selected`       |
| `lighter_background`     | `cursor_bg`      |
| `foreground`             | `cursor_fg`      |
| `selection`              | `border`         |
| `accent`                 | `border_active`  |
| `accent`                 | `title`          |
| `dark_foreground`        | `muted`          |
| `accent`                 | `status`         |
| `dark_foreground`        | `help`           |
| `red`                    | `error`          |
| `green`                  | `success`        |

If a theme slug has no `colors.toml` (some user themes ship only terminal/Hyprland configs), the sync prints an error and leaves the config unchanged.

## Development

```sh
make build        # dist/xuz
make test         # go test ./... + PTY smoke test (python3, no deps beyond stdlib)
make smoke        # PTY smoke test only
make install      # ~/.local/bin/xuz
```

The PTY smoke test (`scripts/pty_smoke.py`) drives the real binary on a
24x100 pseudo-terminal: default selection, cursor + space selection, column
round-trips, alias switching, `--dry-run`, and history preselection. It
needs an environment where `TIOCSWINSZ` works on ptys (most normal systems);
in containers that silently ignore it the TUI stays blank by design — it
refuses to render into a 0x0 terminal — and the hermetic test suite
(`go test ./...`, which drives the real TUI model key-by-key) is the
verification fallback.
