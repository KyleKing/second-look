#!/bin/sh
# Open one of demo/cases/*.toml as a live review, so a layout can be tried by
# hand rather than argued about. Edit the case file, run this again.
#
# The checkout is rebuilt from the case file every run and kept afterwards, so
# what the screen wrote back is readable at demo/.work/<case>/.second-look/.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
name=${1:-}

if [ -z "$name" ] || [ ! -f "$here/cases/$name.toml" ]; then
  echo "usage: $(basename "$0") <case>" >&2
  for f in "$here"/cases/*.toml; do
    echo "  $(basename "$f" .toml)" >&2
  done
  exit 2
fi

go build -o "$here/../second-look" "$here/../cmd/second-look"

work=$here/.work/$name
rm -rf "$work"
mkdir -p "$work"
cp -R "$here/fixture/.second-look" "$work/"
cp "$here/cases/$name.toml" "$work/.second-look/pr-2.toml"

cd "$work"
git init -q .
git commit -q --allow-empty -m "the demo's own history"
git remote add origin https://github.com/KyleKing/second-look.git

FIXTURE="$here/fixture" PATH="$here/bin:$PATH" "$here/../second-look" 2
