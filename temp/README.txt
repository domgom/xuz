xuz layout examples — alignment verification
============================================

These files show the full-screen view (title bar, alias column, one option
column, command preview) for the same config, differing only in the icons:

    aliases:
      llama:    icon: 🦙 or ⚙ or ★ or 🚀 (2/1/1/2 cells)
      tf-plan:  icon: ⚙ or ★ or 🚀 (1/1/2 cells)
      plain:    (no icon)
    every alias has an env group: dev / stag / prod
    state: llama is the current alias (needle ▸), dev is preselected
           (example5: tf-plan is current, prod preselected from history)

THE RULE
--------
Per-column icon slot:
    alias slot  = the max display width of the icons set on the alias column
    env slot    = the max display width of the icons set on the env column
    (0 when the column has no icons; color tags do not affect the width)
Every row in EVERY column is:
    [slot][needle "▸ " or two spaces][name or key]
so all names and keys in a column start at the same cell, in every column.
The two slots are independent: the option column has slot 0 in examples
1-4 even though the alias column has icons, and slot 2 in example 5 where
the env options carry icons.

CELL POSITIONS (1-indexed from the left edge of the file)
--------------------------------------------------------
example1.txt   alias icons 🦙(2) + ⚙(1) + none; env icons none
               -> alias slot 2, env slot 0
    alias names (llama/tf-plan/plain): all start at cell 7
    env keys    (dev/stag/prod):       all start at cell 22
    (ALIAS box 17 wide: border=1, padding=2, slot=3-4, needle=5-6, name=7;
     ENV box 11 wide at cell 18: border=18, padding=19, needle=20-21, key=22)

example2.txt   no icons at all -> alias slot 0, env slot 0
    alias names: all start at cell 5
    env keys:    all start at cell 20
    (no leading slot: border=1, padding=2, needle=3-4, name=5)

example3.txt   alias icons ⚙(1) + ★(1) + none; env icons none
               -> alias slot 1, env slot 0
    alias names: all start at cell 6
    env keys:    all start at cell 21

example4.txt   alias icons 🦙(2) + 🚀(2) + none; env icons none
               -> alias slot 2, env slot 0
    alias names: all start at cell 7
    env keys:    all start at cell 22

example5.txt   alias icons 🦙(2) + ⚙(1) + none; env icons 🏠(2) + ⚠(1)
               -> alias slot 2, env slot 2
    alias names: all start at cell 7
    env keys:    all start at cell 24
    (ENV box: border=18, padding=19, slot=20-21, needle=22-23, key=24)

HOW TO CHECK
------------
Open a file in a monospaced editor and confirm:
  1. the three alias names all start at the same cell, and
  2. the three env keys all start at the same cell, and
  3. those cells match the numbers listed above.
Icon rows and non-icon rows must line up exactly — that is the point.

NOTES
-----
- The needle on the preselected env row (dev; prod in example5) is GREEN in
  the real TUI; when the preselection comes from history it is rendered in
  the theme's MUTED (gray) color instead — the old hourglass badge is gone.
  Colors are not visible in these plain-text files.
- The current alias row is highlighted (bold + background) in the real TUI;
  the ▸ needle marks it here.
- Widths are illustrative minimums; see the note at the top.
