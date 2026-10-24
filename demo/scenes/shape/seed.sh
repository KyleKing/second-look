#!/bin/sh
# Writes a change where eight vendor cards make one edit, two of them do more
# beside it, and two cards were left out. The head is a real commit in the
# scene's checkout, because "not changed at head" searches it, and its id is
# fixed by committing the same tree with the same author and date every run.
set -eu

head=a11c2669ee1446ce5f6540b8c3c93c4cffc3900a
cache=$1
work=$(dirname "$cache")
patch=$cache/diff/$head.patch
: >"$patch"

old_import="import { useSuspenseVendor } from '#features/vendor/dashboard/hooks.js';"
new_import="import { useVendorId } from '#features/vendor/hooks/vendor.js';"
old_id="  const id = useSuspenseVendor(data => data.id);"
new_id="  const id = useVendorId();"

# card name extra: the card at head, and its hunk, which adds extra after the id.
card() {
  name=$1
  extra=${2:-}
  path=web/partner/cards/$name.tsx
  mkdir -p "$work/web/partner/cards"

  {
    echo "$new_import"
    echo "import { EntityCard } from 'src/components/EntityCard';"
    echo
    echo "export function $name() {"
    echo "$new_id"
    [ -z "$extra" ] || echo "$extra"
    echo "  return <EntityCard kind=\"$name\" id={id} />;"
    echo "}"
  } >"$work/$path"

  added=6
  [ -z "$extra" ] || added=7

  {
    printf 'diff --git a/%s b/%s\nindex 1111111..2222222 100644\n--- a/%s\n+++ b/%s\n' "$path" "$path" "$path" "$path"
    printf '@@ -1,6 +1,%d @@\n' "$added"
    echo "-$old_import"
    echo "+$new_import"
    echo " import { EntityCard } from 'src/components/EntityCard';"
    echo " "
    echo " export function $name() {"
    echo "-$old_id"
    echo "+$new_id"
    [ -z "$extra" ] || echo "+$extra"
    echo "   return <EntityCard kind=\"$name\" id={id} />;"
  } >>"$patch"
}

# left name dir: a card at head nobody moved to the route id.
left() {
  mkdir -p "$work/web/partner/$2"
  {
    echo "$old_import"
    echo
    echo "export function $1() {"
    echo "$old_id"
    echo "  return <Panel id={id} />;"
    echo "}"
  } >"$work/web/partner/$2/$1.tsx"
}

for c in NotesCard RadarCard AssessmentsCard DocumentsCard EngagementsCard VendorTabs; do
  card "$c"
done
card ContractsCard "  const { data } = useContracts(id, { limit: CARD_CONTRACT_COUNT });"
card TasksCard "  const { data } = useTasks(id, { limit: 25, mine: true });"
left RisksCard cards
left OverviewTab tabs

mkdir -p "$work/web/partner/hooks"
cat >"$work/web/partner/hooks/vendor.js" <<'JS'
export function useVendorId() {
  return useParams().vendorId;
}
JS
cat >>"$patch" <<'HUNK'
diff --git a/web/partner/hooks/vendor.js b/web/partner/hooks/vendor.js
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/web/partner/hooks/vendor.js
@@ -0,0 +1,3 @@
+export function useVendorId() {
+  return useParams().vendorId;
+}
HUNK

# No config but this script's own reaches the commit, so a signing key or a
# hook on one laptop cannot change the id it gets.
cd "$work"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git init -q .
git add web
GIT_AUTHOR_DATE="2026-10-23T12:00:00Z" GIT_COMMITTER_DATE="2026-10-23T12:00:00Z" \
  git -c user.name=demo -c user.email=demo@example.invalid commit -q -m "the change under review"

got=$(git rev-parse HEAD)
if [ "$got" != "$head" ]; then
  echo "the seed committed $got, not $head; set head here and in pr-view.json" >&2
  exit 1
fi

cat >"$cache/pr-2.toml" <<TOML
version = 1
host = "github.com"
owner = "KyleKing"
repo = "second-look"
number = 2
head_sha = "$head"
event = "COMMENT"
body = ""
TOML

echo '[]' >"$cache/threads/$head.json"
cat >"$cache/about/$head.json" <<'JSON'
{
  "title": "refactor(vendor): start each card from the route id",
  "author": "hubot",
  "body": "Each card read the vendor id off the vendor record, so its own query waited for the vendor to load. They read it from the route now.",
  "labels": [],
  "added": 51,
  "removed": 16,
  "comments": []
}
JSON
