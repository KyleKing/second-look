package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/kyleking/aragonite/tui/keys"
)

// TestKeymap_AgreesWithShared holds second-look to the family keymap: each
// screen's keys, merged with aragonite's shared table, disagree with it
// nowhere and put no reserved key to another use. The mirror it checks is
// held to the bindings the screens dispatch on, so a key added to defaultKeyMap
// and left out of the mirror fails here rather than drifting unchecked, which
// is how u, v, ctrl+r, and y outlived the first family keymap.
func TestKeymap_AgreesWithShared(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		shared []keys.Binding
		tool   []keys.Binding
		moves  keys.Moves
		build  func() (*keys.Keymap, error)
	}{
		{"review", sharedReview(), familyReview(), reviewMoves(), reviewKeymap},
		{"list", sharedList(), familyList(), listMoves(), listKeymap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, d := range keys.Disagreements(tc.shared, tc.tool) {
				t.Error(d)
			}

			km, err := tc.build()
			if err != nil {
				t.Fatalf("the %s keymap does not merge: %v", tc.name, err)
			}

			keys.RequireReserved(t, km)

			for seq := range tc.moves {
				mv, ok := km.Move(keys.MovedAction(seq))
				if !ok || strings.Contains(mv.Notice, "unbound") {
					t.Errorf("%s moved to %q, which this keymap leaves unbound: %q", seq, tc.moves[seq], mv.Notice)
				}
			}
		})
	}
}

// The keys a screen dispatches on that the mirror leaves out on purpose: the
// panes' and the call card's own keys, which own the keyboard while they are
// open, and the arrow and home/end aliases of keys the mirror does carry.
var (
	modalFields = []string{
		"EndPane", "SendSet", "DropQueued", "Expand", "Interrupt", "Callers", "Callees",
	}
	aliasKeys = []string{homeKey, endKey, "left", "right"}
)

// TestKeymap_MirrorsTheDispatch fails a key a screen answers that its mirror
// does not carry, and a moved key a screen still answers.
func TestKeymap_MirrorsTheDispatch(t *testing.T) {
	t.Parallel()

	review, err := reviewKeymap()
	if err != nil {
		t.Fatal(err)
	}

	list, err := listKeymap()
	if err != nil {
		t.Fatal(err)
	}

	mirrors(t, "review", reflect.ValueOf(defaultKeyMap()), review, reviewMoves())
	mirrors(t, "list", reflect.ValueOf(defaultListKeys()), list, listMoves())
}

func mirrors(t *testing.T, name string, v reflect.Value, km *keys.Keymap, moves keys.Moves) {
	t.Helper()

	seqs := liveSequences(km)

	for i := range v.NumField() {
		field := v.Type().Field(i).Name
		if slices.Contains(modalFields, field) {
			continue
		}

		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			continue
		}

		for _, k := range b.Keys() {
			mirrored(t, name, field, k, seqs, moves)
		}
	}
}

// liveSequences is every sequence km binds, split into keys, the moved ones
// left out.
func liveSequences(km *keys.Keymap) [][]string {
	var seqs [][]string

	for _, b := range km.Bindings() {
		if b.Group == keys.GroupMoved {
			continue
		}

		for _, k := range b.Keys {
			seqs = append(seqs, strings.Fields(k))
		}
	}

	return seqs
}

// mirrored fails k, which field answers, where it moved or where no live
// sequence starts with it.
func mirrored(t *testing.T, name, field, k string, seqs [][]string, moves keys.Moves) {
	t.Helper()

	if _, moved := moves[k]; moved {
		t.Errorf("%s: %s still answers %s, which moved", name, field, k)
	}

	if slices.Contains(aliasKeys, k) {
		return
	}

	if !slices.ContainsFunc(seqs, func(s []string) bool { return s[0] == k }) {
		t.Errorf("%s: %s answers %s, which the family mirror does not carry", name, field, k)
	}
}
