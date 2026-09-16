#!/bin/sh
# Open the demo review in a throwaway checkout, so the recording never touches a
# real review and leaves nothing behind. Run by demo.tape, not by hand.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cp -R "$here/fixture/.second-look" "$work/"
cd "$work"
git init -q .
git -c user.name=demo -c user.email=demo@example.invalid commit -q --allow-empty -m "the demo's own history"
git remote add origin https://github.com/KyleKing/second-look.git

# A language server would load this throwaway checkout and draw a count nobody
# recorded. The override turns the checker pass off the same way a laptop
# without one does.
mkdir -p "$work/home/.config/second-look"
cat > "$work/home/.config/second-look/config.toml" <<'CONF'
[[server]]
name = "none"
command = ["second-look-no-such-language-server"]
extensions = [".go", ".ts", ".py"]
CONF

HOME="$work/home" SEED="$here/fixture" PATH="$here/bin:$PATH" "$here/../second-look" 2
