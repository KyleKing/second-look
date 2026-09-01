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
git commit -q --allow-empty -m "the demo's own history"
git remote add origin https://github.com/KyleKing/second-look.git

FIXTURE="$here/fixture" PATH="$here/bin:$PATH" "$here/../second-look" 2
