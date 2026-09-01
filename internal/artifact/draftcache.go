package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DraftPath is where an edit left unfinished waits, one file per thing being
// written. It is beside the review rather than inside it: what is in here is
// not part of the review until somebody saves it, and a half-written sentence
// in the artifact would post.
func DraftPath(root, key string) string {
	return filepath.Join(root, Dir, "drafts", DraftName(key)+".md")
}

// DraftName is the file name for a key, which the caller composes out of what
// is being written. The readable half is kept for anyone reading the directory
// and the digest is what makes two long keys distinct.
func DraftName(key string) string {
	sum := sha256.Sum256([]byte(key))
	digest := hex.EncodeToString(sum[:])[:8]

	var b strings.Builder

	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	const readable = 60

	name := b.String()
	if len(name) > readable {
		name = name[:readable]
	}

	return name + "-" + digest
}

// SaveDraft writes what is being typed. It is called on every keystroke, so it
// writes in place rather than through a temporary file: the loser of a torn
// write is one abandoned sentence, and the cost of the safe version is a file
// created and renamed per character.
func SaveDraft(root, key, body string) error {
	path := DraftPath(root, key)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(body), filePerm); err != nil {
		return fmt.Errorf("keeping the unfinished edit: %w", err)
	}

	return nil
}

// LoadDraft reads back an unfinished edit and says when it was left. Nothing
// here fails: a buffer that cannot be read is a buffer nobody is offered, which
// is where the editor stood before any of this existed.
func LoadDraft(root, key string) (string, time.Time, bool) {
	path := DraftPath(root, key)

	body, err := os.ReadFile(path) //nolint:gosec // the path is composed from the caller's own key
	if err != nil {
		return "", time.Time{}, false
	}

	at := time.Time{}
	if info, err := os.Stat(path); err == nil {
		at = info.ModTime()
	}

	return string(body), at, true
}

// DropDraft removes one, which is what saving or discarding the edit does. A
// buffer nobody will be offered again is a buffer that should not be on disk.
func DropDraft(root, key string) error {
	if err := os.Remove(DraftPath(root, key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("dropping the unfinished edit: %w", err)
	}

	return nil
}
