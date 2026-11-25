# The README's judge clip: a bot's finding ruled todo with the reply drafted,
# a new comment with its severity, and the review published with request
# changes. `mise run demo` builds ./second-look and runs every demo/*.tape. It
# drives demo/scene.sh, which seeds a stand-in gh, so recording needs no
# network, no auth, and touches no real review.

Output docs/images/judge.gif

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
Type "demo/scene.sh judge" Enter
Sleep 6s
Show

# tab is the next thing waiting on a ruling; t marks it todo, and the agent
# drafts the reply because none was written.
Sleep 1500ms
Tab
Sleep 1s
Type "t"
Sleep 2200ms

# i on a line, then the severity, opens a comment in place. It is a vim field:
# esc to normal mode, ciw rewords the first word, and enter keeps it from any
# mode.
Type@70ms "jjjjjjjjjj"
Sleep 600ms
Type "i"
Sleep 1300ms
Type "n"
Sleep 600ms
Type@45ms "Name the line in the error."
Sleep 700ms
Escape
Sleep 900ms
Type@150ms "0"
Sleep 600ms
Type@150ms "ciw"
Sleep 600ms
Type@90ms "Put"
Sleep 900ms
Enter
Sleep 1300ms

# P is the publish page. A draft still waits, so the page goes to it first.
Type "P"
Sleep 1500ms
Type "r"
Sleep 1800ms
Type "r"
Sleep 900ms

# Again: request changes, and say who gets the todo.
Type "P"
Sleep 1000ms
Type "r"
Sleep 1700ms
Type "n"
Sleep 3s

Hide
Type "q"
Sleep 500ms
