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
Set Framerate 15

Env CATPPUCCIN_THEME "ink"

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
