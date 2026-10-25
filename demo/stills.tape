# The README's two stills, docs/images/outline.png and docs/images/calls.png.
# `mise run demo` runs it with every other tape.
#
# Both review this repository's own commits with `second-look local`, so it
# needs gopls on PATH and a checkout that still holds 9066a8f and 8cc3241.

Output demo/.work/stills.gif

Set Shell "bash"
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 18
Set Theme {"name": "Ink", "black": "#2d3340", "red": "#f27a86", "green": "#5fd497", "yellow": "#e8c35a", "blue": "#7aa8ff", "magenta": "#f29ac8", "cyan": "#7fcfc0", "white": "#b6bdc8", "brightBlack": "#343b48", "brightRed": "#ee8f99", "brightGreen": "#5fd497", "brightYellow": "#ffa45c", "brightBlue": "#94a8ff", "brightMagenta": "#c39bff", "brightCyan": "#56c8d8", "brightWhite": "#c8ced8", "background": "#1e2128", "foreground": "#c8ced8", "selection": "#343b48", "cursor": "#c8ced8"}
Set Framerate 15

Env COLORTERM "truecolor"

Hide
Type "export XDG_CONFIG_HOME=$(mktemp -d)" Enter
Type "./second-look local -r 9066a8f" Enter
Sleep 10s
Type "I"
Sleep 1500ms
Show
Sleep 500ms
Screenshot docs/images/outline.png
Sleep 1s
Hide
Type "q"
Sleep 500ms
Type "clear; ./second-look local -r 8cc3241" Enter
Sleep 10s
Type "jjjjjjj"
Sleep 500ms
Type "gc"
Sleep 8s
Show
Sleep 500ms
Screenshot docs/images/calls.png
Sleep 1s
Hide
Type "q"
Sleep 500ms
