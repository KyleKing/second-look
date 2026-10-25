# The README's reading recording: a big change on its outline, then the call
# graph around a function. `mise run demo` runs it with every other tape.
#
# The first half is seed data and needs nothing. The second half reviews this
# repository's own commit 8cc3241 with `second-look local`, so it needs gopls on
# PATH and a checkout that still holds that commit.

Output demo/reading.gif

Set Shell "bash"
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 18
Set Theme {"name": "Ink", "black": "#2d3340", "red": "#f27a86", "green": "#5fd497", "yellow": "#e8c35a", "blue": "#7aa8ff", "magenta": "#f29ac8", "cyan": "#7fcfc0", "white": "#b6bdc8", "brightBlack": "#343b48", "brightRed": "#ee8f99", "brightGreen": "#5fd497", "brightYellow": "#ffa45c", "brightBlue": "#94a8ff", "brightMagenta": "#c39bff", "brightCyan": "#56c8d8", "brightWhite": "#c8ced8", "background": "#1e2128", "foreground": "#c8ced8", "selection": "#343b48", "cursor": "#c8ced8"}
Set Framerate 8
Set TypingSpeed 90ms

Env COLORTERM "truecolor"

Hide
Type "demo/scene.sh outline" Enter
Sleep 6s
Show

# Chapters, each with its read strip and the findings left to judge.
Sleep 2s
Type "j"
Sleep 1s
Type "j"
Sleep 1s

# Open one, and walk its unread hunks.
Type "k"
Sleep 600ms
Type "k"
Sleep 600ms
Type "j"
Sleep 800ms
Enter
Sleep 1800ms
Type " "
Sleep 1200ms
Type "]u"
Sleep 1500ms
Type " "
Sleep 1200ms
Type "]u"
Sleep 1500ms
Type "]u"
Sleep 1500ms

# Back on the outline, the strip has moved.
Type "I"
Sleep 2500ms

Hide
Type "q"
Sleep 500ms
Type "clear; XDG_CONFIG_HOME=$(mktemp -d) ./second-look local -r 8cc3241" Enter
Sleep 10s
Type "jjjjjjj"
Sleep 500ms
Type "gc"
Sleep 8s
Show

# The call graph around the function under the cursor, from gopls.
Sleep 3s
Type "j"
Sleep 1500ms
Type "za"
Sleep 3s

Hide
Type "q"
Sleep 500ms
