// Package seen records which hunks of a review have been read.
//
// A hunk is identified by what it says rather than where it sits: the file it
// belongs to plus every line of the hunk, kinds included, with the line numbers
// left out. A hunk that shifts down the file when something above it grows is
// the same hunk and stays read; a hunk whose text changed is a different one
// and comes back unread, which is the whole point.
package seen

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/kyleking/second-look/internal/diff"
)

// ID is the identity of one hunk, as a hex digest.
type ID string

// Hunk identifies the hunk a diff line belongs to. Lines from one hunk all
// answer the same ID, so marking any line of it marks the hunk.
func Hunk(d *diff.Diff, path string, hunk int) ID {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})

	for i := range d.Files {
		if filePath(&d.Files[i]) != path {
			continue
		}

		for _, l := range d.Files[i].Lines {
			if l.Hunk != hunk {
				continue
			}

			h.Write([]byte{l.Kind})
			h.Write([]byte(l.Text))
			h.Write([]byte{'\n'})
		}
	}

	return ID(hex.EncodeToString(h.Sum(nil)))
}

// Hunks is every hunk in the diff, in patch order, paired with the file it
// belongs to. A file that carries no hunk -- a rename, a binary payload -- has
// nothing to read and so is absent.
func Hunks(d *diff.Diff) []Ref {
	var (
		out  []Ref
		last int
	)

	for i := range d.Files {
		path := filePath(&d.Files[i])
		last = 0

		for _, l := range d.Files[i].Lines {
			if l.Hunk == last {
				continue
			}

			last = l.Hunk
			out = append(out, Ref{Path: path, Hunk: l.Hunk, ID: Hunk(d, path, l.Hunk)})
		}
	}

	return out
}

// Ref names one hunk of one diff.
type Ref struct {
	Path string
	Hunk int
	ID   ID
}

func filePath(f *diff.File) string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}
