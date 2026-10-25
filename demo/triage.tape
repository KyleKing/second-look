# The README's triage recording: the queue in lanes, then judging a bot's
# findings, labeling the pull request for the bot that fixes them, and posting. `mise run demo` builds
# ./second-look and runs this. It drives demo/scene.sh, which seeds a stand-in gh,
# so recording needs no network, no auth, and touches no real review.

Output demo/triage.gif

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
Type "demo/scene.sh inbox" Enter
Sleep 6s
Show

# The queue, in lanes that keep work in flight small.
Sleep 1500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 1500ms

Hide
Type "q"
Sleep 500ms
Type "clear; demo/scene.sh judge" Enter
Sleep 6s
Show

# The bot's first finding: a todo, and the agent drafts the reply.
Sleep 1200ms
Tab
Sleep 1200ms
Type "t"
Sleep 1800ms

# The next one is not worth fixing: resolve it with a drafted reply.
Tab
Tab
Sleep 1000ms
Type "R"
Sleep 2s

# Rule on the draft, then on the one already waiting.
Tab
Sleep 600ms
Type "r"
Sleep 1000ms
Tab
Tab
Sleep 600ms
Type "r"
Sleep 1200ms

# Label the pull request from the repository's own labels, so a bot that
# fixes on a label picks the todos up.
Type "l"
Sleep 1s
Type "fix"
Sleep 800ms
Enter
Sleep 1200ms
Escape
Sleep 400ms

# Submit: request changes, and say who gets the fixes.
Type "S"
Sleep 800ms
Type "r"
Sleep 1400ms
Type "n"
Sleep 1600ms

Type "q"
Sleep 500ms
