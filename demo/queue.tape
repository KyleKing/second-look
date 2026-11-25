# The README's queue clip: the inbox in lanes, the next row that needs a look,
# the merged lane unfolded, then the conversations and staged tabs.
# `mise run demo` builds ./second-look and runs every demo/*.tape. It drives
# demo/scene.sh, which seeds a stand-in gh, so recording needs no network, no
# auth, and touches no real review.

Output docs/images/queue.gif

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
Type "demo/scene.sh inbox" Enter
Sleep 6s
Show

# Lanes keep work in flight small: ready to merge, back to you, to review.
Sleep 2500ms
Type "j"
Sleep 700ms
Type "j"
Sleep 1s

# ]u is the next row needing a look; the side pane says what the row could not.
Type "]u"
Sleep 2s

# The merged lane recedes until za shows it.
Type "za"
Sleep 2500ms

# Conversations that moved, then what is staged on this laptop.
Type "2"
Sleep 3s
Type "3"
Sleep 3s

Hide
Type "q"
Sleep 500ms
