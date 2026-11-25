# The README's reading clip: a change of seventy-two files on its outline, one
# chapter opened, its hunks ticked read with space and walked with ]u, and the
# outline's read strip moved on return. `mise run demo` builds ./second-look
# and runs every demo/*.tape. It drives demo/scene.sh against seed data.

Output docs/images/read.gif

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
Type "demo/scene.sh outline" Enter
Sleep 6s
Show

# Chapters, each with its read strip and the findings left to judge.
Sleep 2500ms
Type "j"
Sleep 900ms
Type "k"
Sleep 800ms

# Open the first, tick a hunk read, and walk to the next unread one.
Enter
Sleep 2s
Space
Sleep 1200ms
Type "]u"
Sleep 1500ms
Space
Sleep 1200ms

# Back on the outline, the chapter's strip has moved.
Type "I"
Sleep 3s

Hide
Type "q"
Sleep 500ms
