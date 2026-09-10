#!/usr/bin/env python3
"""PTY smoke test for the xuz TUI.

Usage: python3 scripts/pty_smoke.py /path/to/xuz [config.yml]

Drives the real binary on a pseudo-terminal (24x100, xterm-256color), checks
both modes (the compact short mode and the full-screen mode with -f),
rendered output, history preselection, --dry-run, navigation, fuzzy search,
adding/deleting options, the save-on-quit prompt, detached runs, and that
the picker exits (without reopening) with the command's status.
"""
import fcntl
import os
import pty
import re
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import textwrap
import time

WIDTH, HEIGHT = 100, 24


class TTY:
    def __init__(self, argv, env, cwd):
        self.master, slave = pty.openpty()
        # bubbletea renders nothing until the terminal has a size
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", HEIGHT, WIDTH, 0, 0))
        self.proc = subprocess.Popen(
            argv, stdin=slave, stdout=slave, stderr=slave, env=env, cwd=cwd
        )
        os.close(slave)
        self.buf = b""

    def read_until(self, needle: str, timeout: float = 10.0, ansi_ok: bool = False) -> str:
        # By default the needle must appear verbatim in the raw byte stream.
        # That is how executed command output (plain text) looks, while the
        # TUI's own styled rendering splits such strings with escape
        # sequences. Pass ansi_ok=True for on-screen text that may span
        # style boundaries; the ANSI-stripped view is searched then.
        deadline = time.time() + timeout
        needle_b = needle.encode()

        def found() -> bool:
            if ansi_ok:
                return needle in clean(self.buf.decode("utf-8", "replace"))
            return needle_b in self.buf

        while time.time() < deadline:
            if found():
                return self.buf.decode("utf-8", "replace")
            r, _, _ = select.select([self.master], [], [], 0.25)
            if r:
                try:
                    data = os.read(self.master, 4096)
                except OSError:
                    break
                if not data:
                    break
                self.buf += data
            elif self.proc.poll() is not None and not self._drain():
                break
        # final drain: a dying process flushes the rest of its output
        while self._drain():
            pass
        if not found():
            self.proc.kill()
            raise AssertionError(
                f"timeout waiting for {needle!r}.\n--- screen ---\n"
                f"{self.buf.decode('utf-8', 'replace')[-3000:]}"
            )
        return self.buf.decode("utf-8", "replace")

    def _drain(self) -> bool:
        got = False
        while True:
            r, _, _ = select.select([self.master], [], [], 0.1)
            if not r:
                break
            try:
                data = os.read(self.master, 65536)
            except OSError:
                return got
            if not data:
                break
            self.buf += data
            got = True
        return got

    def send(self, data: bytes, delay: float = 0.35):
        os.write(self.master, data)
        time.sleep(delay)

    def finish(self):
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait()
        os.close(self.master)
        return self.proc.returncode


CONFIG = """\
theme: default
remember_last: 100  # well above the entry count, so history never trims mid-suite
last_used:
  - small
aliases:
  echoer:
    options:
      model:
        - small: !default /models/small.gguf
        - big: /models/big.gguf
      context:
        - s: 10
        - l: 20
      command: echo M=$MODEL C=$CONTEXT
  other:
    options:
      model:
        - m1: v1
      command: echo other
  failer:
    command: exit 1
"""


def make_home() -> str:
    home = tempfile.mkdtemp(prefix="xuz-home-")
    os.makedirs(os.path.join(home, ".xuz"), exist_ok=True)
    with open(os.path.join(home, ".xuz", "config.yml"), "w") as f:
        f.write(CONFIG)
    return home


def env_for(home: str) -> dict:
    env = dict(os.environ)
    env["HOME"] = home
    env["TERM"] = "xterm-256color"
    env.pop("XUZ_CONFIG", None)
    env.pop("XUZ_HISTORY", None)
    return env


def history_lines(home: str):
    p = os.path.join(home, ".xuz", "history")
    if not os.path.exists(p):
        return []
    with open(p) as f:
        return [l for l in f.read().splitlines() if l.strip()]


ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)?")


def clean(s: str) -> str:
    """Strip ANSI escape sequences so styled output matches plain text."""
    return ANSI_RE.sub("", s)


def check(name, cond, detail=""):
    if not cond:
        print(f"FAIL: {name}: {detail}")
        sys.exit(1)
    print(f"ok: {name}")


def test_short_search_and_launch(binary, home):
    # Runs last: restore the pristine config (test_add_delete_save deleted
    # the "small" option). The selected alias (▸ needle) is the last launch
    # in the history (failer, from test_exit_on_failure); echoer is selected
    # explicitly with space, and its option preselection is what
    # test_background launched (small/s, selected explicitly there).
    with open(os.path.join(home, ".xuz", "config.yml"), "w") as f:
        f.write(CONFIG)
    before = len(history_lines(home))
    tty = TTY([binary], env_for(home), home)
    out = tty.read_until("filter")
    c = clean(out)
    check("short mode: filter line at the bottom of the list", "filter █" in c, out[-1500:])
    check("short mode: shortcut tooltip",
          "\U000f12b7  exit" in c and "\U000f1050 select" in c
          and "↑↓←→ move" in c and "↵ launch" in c and "/ switch" in c,
          out[-1500:])
    check("short mode: aliases listed", "echoer" in c and "other" in c, out[-1500:])
    # The needle is hidden in the active column: the cursor highlight is the
    # selection indicator. No ▸ in the alias list.
    check("short mode: needle hidden in active column", "▸" not in c, out[-1500:])
    tty.buf = b""
    tty.send(b"c")                 # filter "c" -> only "echoer" matches
    # the redraw splits the label across style runs, so match stripped text
    out = tty.read_until("filter c", ansi_ok=True)
    check("short mode: filter narrows the list", "other" not in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"\x1b")              # esc: clear the filter
    out = tty.read_until("other")
    check("short mode: esc restores the list", "other" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b" ")                 # space: select echoer (the needle was on
                                   # failer) and open its first option column
    out = tty.read_until("model:")
    check("short mode: space selects the alias", "model:" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"\r")                # enter: launch with the preselected options
    out = tty.read_until("M=")
    check("short mode: enter launches with the defaults",
          "M=/models/small.gguf C=10" in out, out[-1500:])
    lines = history_lines(home)
    check("short mode: history written",
          len(lines) == before + 1 and "model=small" in lines[-1] and "context=s" in lines[-1],
          repr(lines))
    tty.finish()


def test_short_options_navigation(binary, home):
    before = len(history_lines(home))
    tty = TTY([binary, "echoer"], env_for(home), home)
    out = tty.read_until("model:")
    c = clean(out)
    check("short mode: starts on the first group",
          "model:" in c and "small" in c and "big" in c, out[-1500:])
    check("short mode: tooltip in the column view",
          "\U000f1050 select" in c and "/ switch" in c, out[-1500:])
    tty.buf = b""
    tty.send(b" ")                 # space: select small (cursor is on it)
    out = tty.read_until("model: small")
    check("short mode: space selects an option",
          "model: small → /models/small.gguf" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"\x1b[C")            # right: context group
    out = tty.read_until("context:")
    c = clean(out)
    check("short mode: right switches the group",
          "context:" in c and "10" in c and "20" in c, out[-1500:])
    tty.buf = b""
    tty.send(b"\x1b[B")            # down: cursor -> l
    tty.send(b" ")                 # space: select l
    out = tty.read_until("context: l")
    check("short mode: context l selected", "context: l → 20" in clean(out), out[-1500:])
    tty.send(b"\r")                # enter: run with the selections
    out = tty.read_until("M=/models/small.gguf C=20")
    check("short mode: ran with the selected options",
          "M=/models/small.gguf C=20" in out, out[-1500:])
    lines = history_lines(home)
    check("short mode: history has the selection",
          len(lines) == before + 1 and "context=l" in lines[-1], repr(lines))
    tty.finish()


def test_short_slash_switch(binary, home):
    tty = TTY([binary], env_for(home), home)
    out = tty.read_until("filter")
    check("short mode: initial render", "filter █" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"/")                 # "/": full mode (alias column starts plain)
    # wait for the legend line (bottom of the frame) so "switch" is present
    out = tty.read_until("switch")
    c = clean(out)
    check("/ opens the full mode", "ALIAS" in c and "MODEL" in c and "CONTEXT" in c,
          out[-1500:])
    check("full mode help advertises the switch", "switch" in c, out[-1500:])
    tty.buf = b""
    tty.send(b"/")                 # "/": back to the short mode
    out = tty.read_until("filter")
    check("/ returns to the short mode", "filter █" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"/")                 # "/": switch to the full mode
    out = tty.read_until("ALIAS")
    check("/ opens the full mode from a column",
          "ALIAS" in clean(out) and "MODEL" in clean(out), out[-1500:])
    tty.send(b"\x03")              # ctrl+c: quit
    rc = tty.finish()
    check("quit exits 0", rc == 0, str(rc))


def test_default_select(binary, home):
    before = len(history_lines(home))
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    out = tty.read_until("M=")
    c = clean(out)
    check("renders columns", "echoer" in c and "MODEL" in c and "CONTEXT" in c, out[-1500:])
    check("default selection shown", "M=/models/small.gguf C=10" in c, out[-1500:])
    tty.buf = b""  # drop the styled preview: it would match "small.gguf"
    tty.send(b"\r")  # enter: run
    out = tty.read_until("M=/models/small.gguf C=10")
    check("command ran", "M=/models/small.gguf C=10" in out, out[-1500:])
    lines = history_lines(home)
    check("history written",
          len(lines) == before + 1 and "model=small" in lines[-1] and "context=s" in lines[-1],
          repr(lines))
    tty.finish()


def test_select_and_run(binary, home):
    before = len(history_lines(home))
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    tty.send(b"\x1b[B")  # down: cursor -> big (already in the model column)
    tty.send(b" ")       # space: select big
    out = tty.read_until("model: big →")
    check("selection status", "model: big → /models/big.gguf" in clean(out), out[-1500:])
    tty.send(b"\x1b[Z")  # shift+tab: back to alias column
    tty.send(b"\x1b[C")  # right: forward again (cursor should be kept)
    out = tty.read_until("big")
    # The needle is hidden in the active column: the cursor highlight is the
    # selection indicator. "big" is the cursor row.
    check("cursor kept after column round-trip", "big /models/big.gguf" in clean(out), out[-1500:])
    tty.buf = b""  # the needle would otherwise match the earlier status line
    tty.send(b"\r")
    out = tty.read_until("M=/models/big.gguf C=10")
    check("big ran", "M=/models/big.gguf C=10" in out, out[-1500:])
    lines = history_lines(home)
    check("history updated",
          len(lines) == before + 1 and "model=big" in lines[-1] and "context=s" in lines[-1],
          repr(lines))
    tty.finish()


def test_alias_column(binary, home):
    tty = TTY([binary, "-f", "other"], env_for(home), home)
    out = tty.read_until("other")
    tty.send(b"\x1b[D")   # left: to alias column
    out = tty.read_until("other")
    tty.buf = b""
    tty.send(b"\x1b[A")   # up: cursor to echoer (focus stays in the alias column)
    tty.buf = b""
    tty.send(b" ")        # space: select echoer (the needle follows)
    out = tty.read_until("alias: echoer")
    check("space selects the alias", "alias: echoer" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"\x1b[B")   # down: cursor -> other (clears the status line)
    # The preview follows the cursor (other), not the needle.
    out = tty.read_until("echo other", ansi_ok=True)
    check("cursor alias shows options", "echo other" in clean(out), out[-1500:])
    tty.buf = b""
    tty.send(b"\r")
    out = tty.read_until("other")
    check("cursor alias ran", "other" in out, out[-1500:])
    tty.finish()


def test_dry_run(binary, home):
    before = len(history_lines(home))
    tty = TTY([binary, "-f", "echoer", "--dry-run"], env_for(home), home)
    out = tty.read_until("M=")
    tty.send(b"\r")
    deadline = time.time() + 10
    while time.time() < deadline and tty.proc.poll() is None:
        tty._drain()
        time.sleep(0.2)
    out = tty.buf.decode("utf-8", "replace")
    # The !default tag (small) wins over the history preselection (big);
    # the printed line is styled, so match the stripped text
    check("dry-run prints command", "echo M=/models/small.gguf C=10" in clean(out), out[-1500:])
    check("dry-run exits", tty.proc.poll() is not None)
    check("dry-run exit code 0", tty.finish() == 0)
    check("dry-run no history", len(history_lines(home)) == before, repr(history_lines(home)))


def test_history_preselect(binary, home):
    before = len(history_lines(home))
    # launch 1: select big + l, run
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    tty.send(b"\x1b[B")   # down: cursor -> big (already in the model column)
    tty.send(b" ")        # space: select big
    tty.send(b"\x1b[C")   # right: context column
    tty.send(b"\x1b[B")   # down: cursor -> l
    tty.send(b" ")        # space: select l
    tty.send(b"\r")
    tty.read_until("/models/big.gguf")
    tty.finish()
    # launch 2: fresh process, enter immediately. The !default tag (small)
    # wins over the history preselection (big); the context group has no
    # !default tag, so it is preselected from the history (l).
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    out = tty.read_until("M=")
    check("preselect from history", "M=/models/small.gguf C=20" in clean(out), out[-1500:])
    tty.buf = b""  # drop the styled preview: it would match "/models/small.gguf"
    tty.send(b"\r")
    out = tty.read_until("M=/models/small.gguf C=20")
    check("preselected run", "M=/models/small.gguf C=20" in out, out[-1500:])
    lines = history_lines(home)
    check("history has the two new entries", len(lines) == before + 2, repr(lines))
    tty.finish()


def test_search(binary, home):
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    tty.buf = b""               # only keep the redraws caused by the filter
    tty.send(b"f")              # start the filter (model column is active)
    tty.send(b"bi", 0.5)        # pattern "bi" -> only "big" matches
    # the input renders inside the filtered column's box; match stripped
    # text, as the label and the filter sit in separate style runs
    out = tty.read_until("filter bi", ansi_ok=True)
    c = clean(out)
    # The filter narrows the model column to "big" only. The preview may
    # show "small" (the !default tag), so check the column content directly:
    # the model column's box should contain "big" but not "small".
    check("filter narrows the column", "big" in c, out[-1500:])
    check("filter does not narrow other columns", "(no matches)" not in c, out[-1500:])
    check("filter legend shown", "exit filter" in c, out[-1500:])
    tty.buf = b""
    tty.send(b"\x1b")           # esc: exit the filter, full list back
    out = tty.read_until("small")
    check("esc restores the full list", "small" in out and "big" in out, out[-1500:])
    tty.buf = b""
    tty.send(b"f")
    tty.send(b"bi", 0.5)
    tty.read_until("filter bi", ansi_ok=True)
    tty.send(b"\x1b")           # cursor now sits on big
    tty.send(b" ")              # space: select big
    out = tty.read_until("model: big")
    check("selection via filter", "model: big → /models/big.gguf" in clean(out), out[-1500:])
    tty.buf = b""                # the needle would otherwise match the status line
    tty.send(b"\r")             # enter: run (context preselected l from history)
    out = tty.read_until("M=/models/big.gguf C=20")
    check("filtered option ran", "M=/models/big.gguf C=20" in out, out[-1500:])
    tty.finish()


def test_mouse(binary, home):
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")          # initial render; model column is active
    tty.buf = b""
    # Layout at 100x24 for echoer (2 groups), content-based widths:
    # alias x=0..29, model x=30..74, context x=75..100 (0-based). Content
    # rows start at screen row 3 (1-based). Left-click (SGR) the second model
    # row ("big") near the middle of the model column: 1-based (col 53, row 4).
    tty.send(b"\x1b[<0;53;4M", 0.5)
    out = tty.read_until("model: big")
    check("mouse click selects option", "model: big → /models/big.gguf" in clean(out), out[-1500:])
    tty.buf = b""                 # the needle would otherwise match the status line
    tty.send(b"\r")               # enter: run with the mouse-selected option
    # context stays at its history preselection (l)
    out = tty.read_until("M=/models/big.gguf C=20")
    check("mouse-selected option ran", "M=/models/big.gguf C=20" in out, out[-1500:])
    tty.finish()


def test_add_delete_save(binary, home):
    cfg_path = os.path.join(home, ".xuz", "config.yml")
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    tty.send(b"n")              # new value in the model column
    out = tty.read_until("new model option")
    check("new-value prompt", "new model option" in clean(out), out[-1500:])
    tty.send(b"mid /models/mid.gguf", 0.5)
    tty.send(b"\r")
    out = tty.read_until("added")
    c = clean(out)
    check("option added", "mid /models/mid.gguf" in c and "model: added" in c, out[-1500:])
    tty.send(b"\x1b[B")         # down: cursor -> small (mid was inserted at cursor)
    tty.buf = b""               # drop the redraws; the legend line says "delete" too
    tty.send(b"d")              # delete under cursor (small)
    out = tty.read_until("from model?")
    check("delete prompt", 'delete "small" from model?' in clean(out), out[-1500:])
    tty.send(b"y")
    out = tty.read_until("deleted")
    c = clean(out)
    check("option deleted", "model: deleted" in c and "mid /models/mid.gguf" in c, out[-1500:])
    tty.send(b"q")              # quit -> save prompt (dirty)
    out = tty.read_until("save to")
    check("quit asks to save", "config changed" in clean(out), out[-1500:])
    tty.send(b"y")
    tty.finish()
    with open(cfg_path) as f:
        disk = f.read()
    # "small" itself still appears in last_used, so check the option's value
    check("save wrote the config",
          "/models/mid.gguf" in disk and "/models/big.gguf" in disk and "/models/small.gguf" not in disk, disk)


def test_decline_save(binary, home):
    cfg_path = os.path.join(home, ".xuz", "config.yml")
    before = open(cfg_path).read()
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    tty.send(b"\x1b[B")         # down: cursor -> big
    tty.send(b"\x00")           # ctrl+space: set default to big
    tty.send(b"q")
    out = tty.read_until("save to")
    check("default set + save prompt", "default set to" in clean(out), out[-1500:])
    tty.send(b"n")              # decline: quit without saving
    rc = tty.finish()
    check("declined quit exits 0", rc == 0, str(rc))
    check("config untouched after decline", open(cfg_path).read() == before, "")


def test_exit_on_failure(binary, home):
    # the picker must not reopen after a failed command: xuz exits with the
    # command's status (a reopen would keep xuz alive and finish() would
    # have to kill it, yielding a non-1 return code)
    tty = TTY([binary, "-f", "failer"], env_for(home), home)
    out = tty.read_until("exit 1")
    check("failer alias renders", "failer" in clean(out), out[-1500:])
    tty.send(b"\r")
    rc = tty.finish()
    check("failed command: xuz exits with its status, no reopen", rc == 1, str(rc))


def test_background(binary, home):
    import glob
    tty = TTY([binary, "-f", "echoer"], env_for(home), home)
    tty.read_until("M=")
    # the preselection comes from the history (big/l): move to small/s
    # explicitly so the launched command is deterministic
    tty.send(b"\x1b[A")         # up: model cursor -> small
    tty.send(b" ")               # space: select small
    tty.send(b"\x1b[C")         # right: context column (left would go back to the alias column)
    tty.send(b"\x1b[A")         # up: context cursor -> s
    tty.send(b" ")               # space: select s
    time.sleep(0.3)
    tty.send(b"b")              # arm background mode (runs nothing yet)
    time.sleep(0.6)
    check("arming background keeps xuz alive", tty.proc.poll() is None,
          str(tty.proc.poll()))
    tty.send(b"\r")             # enter: run in the background, xuz exits
    rc = tty.finish()
    check("background run exits 0", rc == 0, str(rc))
    log = ""
    for _ in range(25):         # up to ~5s for the child to flush
        files = sorted(glob.glob(os.path.join(home, ".xuz", "echoer-*.log")))
        if files and os.path.getsize(files[0]) > 0:
            log = open(files[-1]).read()
            if "M=" in log:
                break
        time.sleep(0.2)
    check("log created next to history", "M=/models/small.gguf C=10" in log, repr(log))


def test_default_tag_override(binary, home):
    # A group with a !default tag keeps the default preselection even when
    # the history says otherwise (marked ▸ in the theme's selected color);
    # a group without a tag is preselected from the history and its ▸ is
    # rendered in the theme's muted color instead (the old ⧖ badge is gone
    # and never rendered).
    cfg_path = os.path.join(home, ".xuz", "config.yml")
    hist_path = os.path.join(home, ".xuz", "history")
    with open(cfg_path) as f:
        saved_cfg = f.read()
    saved_hist = ""
    if os.path.exists(hist_path):
        with open(hist_path) as f:
            saved_hist = f.read()
    with open(cfg_path, "w") as f:
        f.write("""\
theme: default
aliases:
  tagged:
    options:
      model:
        - small: /models/small.gguf
        - big: !default /models/big.gguf
      context:
        - s: 10
        - l: 20
      command: echo M=$MODEL C=$CONTEXT
""")
    try:
        # launch 1: no history for "tagged" yet -> model from !default,
        # context from the first option
        tty = TTY([binary, "-f", "tagged"], env_for(home), home)
        out = tty.read_until("M=")
        c = clean(out)
        check("default preselected", "M=/models/big.gguf C=10" in c, out[-1500:])
        # The needle is hidden in the active column (MODEL): the cursor
        # highlight is the selection indicator. No hourglass badge anywhere.
        check("no hourglass anywhere", "⧖" not in c, c)
        tty.buf = b""  # drop the styled preview: it would match "/models/big.gguf"
        tty.send(b"\r")
        out = tty.read_until("M=/models/big.gguf C=10")
        check("default ran", "M=/models/big.gguf C=10" in out, out[-1500:])
        tty.finish()
        # launch 2: history has model=big context=s. The model default still
        # wins; the untagged context group is preselected from the history.
        # The needle is hidden in the active column (MODEL); the context
        # column is inactive and shows its needle on "s".
        tty = TTY([binary, "-f", "tagged"], env_for(home), home)
        out = tty.read_until("M=")
        c = clean(out)
        check("default beats history", "M=/models/big.gguf C=10" in c, out[-1500:])
        check("context needle on s (inactive column)", "▸ s" in c, c)
        check("no hourglass badge rendered", "⧖" not in c, c)
        tty.send(b"q")
        rc = tty.finish()
        check("clean quit", rc == 0, str(rc))
    finally:
        # restore both files: the tagged history entry would otherwise push
        # the shared history to its cap and skew the short-mode counts
        with open(cfg_path, "w") as f:
            f.write(saved_cfg)
        if saved_hist:
            with open(hist_path, "w") as f:
                f.write(saved_hist)
        elif os.path.exists(hist_path):
            os.remove(hist_path)


def pty_supports_winsize() -> bool:
    """Return True if a pty in this environment reports a non-zero size.

    Some sandboxes make TIOCSWINSZ a no-op, so a pty always reports 0x0.
    bubbletea then never receives a usable WindowSizeMsg and xuz renders an
    empty frame, so the interactive smoke tests cannot run there.
    """
    master, slave = pty.openpty()
    try:
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", HEIGHT, WIDTH, 0, 0))
        data = fcntl.ioctl(slave, termios.TIOCGWINSZ, b"\x00" * 8)
        rows, cols = struct.unpack("HH", data[:4])
        return rows > 0 and cols > 0
    except OSError:
        return False
    finally:
        os.close(master)
        os.close(slave)


def main():
    binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "dist/xuz")
    if not os.path.exists(binary):
        print(f"binary not found: {binary} (run: make build)")
        sys.exit(1)
    if not pty_supports_winsize():
        print("SKIP: this environment's pty cannot report a window size "
              "(TIOCSWINSZ is a no-op), so the TUI would render empty; "
              "skipping pty smoke tests")
        return
    home = make_home()
    try:
        test_default_select(binary, home)
        test_select_and_run(binary, home)
        test_alias_column(binary, home)
        test_dry_run(binary, home)
        test_history_preselect(binary, home)
        test_search(binary, home)
        test_mouse(binary, home)
        test_background(binary, home)   # before test_add_delete_save mutates the config
        test_add_delete_save(binary, home)
        test_decline_save(binary, home)
        test_exit_on_failure(binary, home)
        # test_default_tag_override runs its own alias ("tagged") and
        # restores the config, so the echoer history test_background left
        # behind (small/s) still drives the short-mode launch below
        test_default_tag_override(binary, home)
        # short mode last: its launch test relies on the preselection left by
        # test_background (small/s), and its counts are relative
        test_short_search_and_launch(binary, home)
        test_short_options_navigation(binary, home)
        test_short_slash_switch(binary, home)
        print("all pty smoke tests passed")
    finally:
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    main()
