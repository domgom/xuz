#!/usr/bin/env python3
"""Repro: multiline command with backslash continuation in the xuz TUI."""
import fcntl
import os
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time

WIDTH, HEIGHT = 42, 20

CONFIG = """\
theme: default
aliases:
  llama:
    options:
      model:
        - qwen: /home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf
      context:
        - 64k: 65536
      command: exec llama-server -m "$MODEL" \\
          -c "$CONTEXT"
"""

ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)?")


def clean(s: str) -> str:
    return ANSI_RE.sub("", s)


def main():
    binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "dist/xuz")
    home = tempfile.mkdtemp(prefix="xuz-home-")
    os.makedirs(os.path.join(home, ".xuz"), exist_ok=True)
    with open(os.path.join(home, ".xuz", "config.yml"), "w") as f:
        f.write(CONFIG)
    env = dict(os.environ)
    env["HOME"] = home
    env["TERM"] = "xterm-256color"
    env.pop("XUZ_CONFIG", None)
    env.pop("XUZ_HISTORY", None)

    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", HEIGHT, WIDTH, 0, 0))
    proc = subprocess.Popen([binary, "-f"], stdin=slave, stdout=slave, stderr=slave, env=env, cwd=home)
    os.close(slave)
    buf = b""
    deadline = time.time() + 8
    while time.time() < deadline:
        r, _, _ = select.select([master], [], [], 0.25)
        if r:
            try:
                data = os.read(master, 65536)
            except OSError:
                break
            if not data:
                break
            buf += data
        else:
            break
    time.sleep(0.5)
    while True:
        r, _, _ = select.select([master], [], [], 0.2)
        if not r:
            break
        try:
            data = os.read(master, 65536)
        except OSError:
            break
        if not data:
            break
        buf += data
    proc.send_signal(3)
    try:
        proc.wait(timeout=3)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    os.close(master)
    text = clean(buf.decode("utf-8", "replace"))
    # print the last screen-ish chunk
    print("=== RAW CLEANED (tail) ===")
    print(text[-2500:])
    print("=== LINES ===")
    for i, line in enumerate(text.splitlines()):
        print(f"{i:3d}|{line}")


if __name__ == "__main__":
    main()
