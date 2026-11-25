# The README's views clip: a pull request read at its fourth head, H comparing
# against an earlier round to say which todos were answered, ]r stepping
# rounds, and \\ walking the views (the diff, the code, the comments).
# `mise run demo` builds ./second-look and runs every demo/*.tape. It drives
# demo/scene.sh against seed data.

Output docs/images/views.gif

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
Type "demo/scene.sh rounds" Enter
Sleep 6s
Show

# H lists the rounds this review was read at, with the todos each answered.
Sleep 2s
Type "H"
Sleep 2500ms
Type "1"
Sleep 2500ms

# ]r steps to the next round.
Type "]r"
Sleep 2500ms

# \ is what is shown; a second \ walks the views.
Type `\`
Sleep 2s
Type `\`
Sleep 2500ms
Type `\\`
Sleep 3s
Type `\\`
Sleep 2s

Hide
Type "q"
Sleep 500ms
