#!/bin/sh
# The queue paints this snapshot first. The searches then answer in the fixture's
# order, so rows move, one arrives, and one is gone.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
work=$(dirname "$1")

# Built under the real HOME: the scene's own has no module cache.
(cd "$root" && go build -o "$work/seedsnap" ./demo/seedsnap)
HOME="$work/home" "$work/seedsnap" "$root/demo/fixture"
rm "$work/seedsnap"
