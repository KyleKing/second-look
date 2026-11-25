# The README's local clip: second-look reviewing this repository's own commit
# 8cc3241 before it would go up: its chapters, then gc asking gopls for the call
# graph around a function and za for the call sites.
# `mise run demo` builds ./second-look and runs every demo/*.tape. It needs gopls
# on PATH and a checkout that still holds 8cc3241.
#
# -r names a jj revset and --range a git range, and each refuses the other
# backend, so the hidden command picks by whether the clone has a .jj. A state
# directory of its own holds a coach ledger that has seen the welcome card, so
# a first launch on this machine does not open behind it.

Output docs/images/local.gif

Set Shell "bash"
Set FontSize 15
Set Width 1180
Set Height 640
Set Padding 18
Set Theme {"name": "Ink", "black": "#2d3340", "red": "#f27a86", "green": "#5fd497", "yellow": "#e8c35a", "blue": "#7aa8ff", "magenta": "#f29ac8", "cyan": "#7fcfc0", "white": "#b6bdc8", "brightBlack": "#343b48", "brightRed": "#ee8f99", "brightGreen": "#5fd497", "brightYellow": "#ffa45c", "brightBlue": "#94a8ff", "brightMagenta": "#c39bff", "brightCyan": "#56c8d8", "brightWhite": "#c8ced8", "background": "#1e2128", "foreground": "#c8ced8", "selection": "#343b48", "cursor": "#c8ced8"}
Set Framerate 8
Set TypingSpeed 90ms

Env COLORTERM "truecolor"
Env ARAGONITE_ICONS "unicode"

Hide
Type `export XDG_STATE_HOME=$(mktemp -d); mkdir -p "$XDG_STATE_HOME/second-look"; echo '{"first":"2026-01-01T00:00:00Z","welcomed":"2026-01-01T00:00:00Z"}' > "$XDG_STATE_HOME/second-look/coach.json"` Enter
Type "export XDG_CONFIG_HOME=$(mktemp -d); clear" Enter
Type "./second-look local $(test -d .jj && echo -r 8cc3241 || echo --range 8cc3241~..8cc3241)" Enter
# The chapters can reorder while gopls loads the project, so the screen settles
# before it is shown and the cursor lands on the same function every run.
Sleep 15s
Show

# One commit in chapters, opening on the edit it made the same way in five files.
Sleep 3s

# gc on a function: its callers and callees, from the language server.
Type@120ms "jjjjjjjjj"
Sleep 800ms
Type "gc"
Sleep 5s
Type "j"
Sleep 1200ms
Type "za"
Sleep 3500ms

Hide
Type "q"
Sleep 500ms
