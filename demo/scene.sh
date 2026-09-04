#!/bin/sh
# Open one of demo/scenes/* as a live screen against seed data, so what the app
# does in a situation is settled by looking at it rather than argued about.
#
# A scene is a directory holding what it changes about the shared fixture: the
# argv second-look is run with, a prepared review, its conversations, extra gh
# routes, and any seed those routes name. The checkout is rebuilt every run and
# kept afterwards, so what the screen wrote back is readable at
# demo/.work/<scene>/.second-look/.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
name=${1:-}
scene=$here/scenes/$name

if [ -z "$name" ] || [ ! -f "$scene/scene.conf" ]; then
  echo "usage: $(basename "$0") <scene>" >&2
  for f in "$here"/scenes/*/scene.conf; do
    dir=$(dirname "$f")
    printf '  %-16s %s\n' "$(basename "$dir")" "$(sed -n 's/^about=//p' "$f")" >&2
  done
  exit 2
fi

args=$(sed -n 's/^args=//p' "$scene/scene.conf")

go build -o "$here/../second-look" "$here/../cmd/second-look"

work=$here/.work/$name
rm -rf "$work"
mkdir -p "$work"
cp -R "$here/fixture/.second-look" "$work/"

# A scene brings a prepared review under whichever name it wants staged, and one
# that brings none reads the fixture's, which is what the queue scenes want.
if [ -f "$scene/review.toml" ]; then
  cp "$scene/review.toml" "$work/.second-look/pr-2.toml"
fi

if [ -f "$scene/no-review" ]; then
  rm -f "$work"/.second-look/pr-*.toml
fi

if [ -f "$scene/threads.json" ]; then
  for recorded in "$work"/.second-look/threads/*.json; do
    cp "$scene/threads.json" "$recorded"
  done
fi

cd "$work"
git init -q .
git -c user.name=demo -c user.email=demo@example.invalid commit -q --allow-empty -m "the demo's own history"
git remote add origin https://github.com/KyleKing/second-look.git

# The scene's own seeds are searched before the shared ones, so overriding an
# answer is dropping a file of the same name into the scene.
# shellcheck disable=SC2086 # args is a command line, so it has to split
HOME="$work/home" XDG_CONFIG_HOME="$work/config" SEED="$scene $here/fixture" \
  PATH="$here/bin:$PATH" "$here/../second-look" $args
