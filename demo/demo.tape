# The README recording. `mise run demo` builds ./second-look and runs this.
#
# It drives demo/run.sh, which opens a prepared review in a throwaway checkout
# with a stand-in gh, so recording it needs no network, no auth, and touches no
# real review. Regenerate it whenever the screen changes shape.

Output demo/second-look.gif

Set Shell "bash"
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 18
Set Framerate 24
Set TypingSpeed 90ms

Hide
Type "./demo/run.sh" Enter
Sleep 4s
Show

Sleep 1500ms

# The review's own note, which starts folded because it is the run log.
Type "za"
Sleep 2s
Type "za"
Sleep 500ms

# The first comment, and the evidence under it.
Type "]c"
Sleep 2s
Type "za"
Sleep 2500ms

# It arrived as a draft, because an agent staged it. Rule on it.
Type "m"
Sleep 1200ms
Type "r"
Sleep 1500ms

# Fixing a word does not mean leaving the screen.
Type "e"
Sleep 1200ms
Type " Wrapping it names the file that failed."
Sleep 800ms
Ctrl+S
Sleep 2s

# The same review as the code that results, with what is said about it folded.
Type "c"
Sleep 2500ms
Type "za"
Sleep 2500ms

# And what will post, by file.
Type "c"
Sleep 2500ms

Type "q"
Sleep 1s
