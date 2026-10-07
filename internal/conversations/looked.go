package conversations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// Looked is when you last read each conversation.
//
// It is the one piece of state this feature keeps, and it is what makes "new"
// mean new to you rather than new to GitHub. A conversation whose last comment
// arrived after you looked is unread however many notifications went out for it.
type Looked struct {
	at map[string]time.Time
}

// NewLooked is a set with nothing read, which is what the first run has.
func NewLooked() *Looked { return &Looked{at: map[string]time.Time{}} }

// record is one entry on disk. The key is what matters; the rest is there so a
// person reading the file can tell which conversation a line refers to.
type record struct {
	Key    string    `toml:"key"`
	Looked time.Time `toml:"looked"`
	Where  string    `toml:"where,omitempty"`
	Anchor string    `toml:"anchor,omitempty"`
}

type file struct {
	Conversation []record `toml:"conversation"`
}

// LookedPath is where the queue's read marks live.
//
// It sits under the user's config directory rather than in a repository,
// because the queue spans repositories and a mark written into whichever
// checkout happened to be open would be lost to every other one.
func LookedPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}

	return filepath.Join(dir, "second-look", "conversations.toml"), nil
}

// LoadLooked reads the marks. A missing file is an empty set, not an error: a
// queue nobody has opened yet is the normal first case, and on that run every
// conversation is honestly new.
func LoadLooked(path string) (*Looked, error) {
	l := NewLooked()

	records, err := readRecords(path)
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		l.at[r.Key] = r.Looked
	}

	return l, nil
}

// readRecords is the file as written, so a save can carry a record it has no
// conversation for rather than dropping the mark on the floor.
func readRecords(path string) ([]record, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the user config directory plus a constant
	if os.IsNotExist(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var f file

	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return f.Conversation, nil
}

// SaveLooked writes the marks this session holds for conversations still in
// the queue, merged over whatever the file already holds. Two shells open at
// once mark different conversations, and overwriting the file with this
// session's view would erase the other's; the newer mark wins a conversation
// both marked, and a mark the file holds for one this session never fetched is
// kept, since the other session may be holding it live.
func SaveLooked(path string, l *Looked, live []Conversation) error {
	earlier, err := readRecords(path)
	if err != nil {
		return err
	}

	f := mergedFile(l, earlier, live)

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	body, err := toml.Marshal(f)
	if err != nil {
		return fmt.Errorf("encoding the read conversations: %w", err)
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// mergedFile folds this session's marks over what the file already held, per
// the merge SaveLooked documents, and returns what the file should now say.
func mergedFile(l *Looked, earlier []record, live []Conversation) file {
	merged := NewLooked()
	stray := map[string]record{}

	for _, r := range earlier {
		merged.at[r.Key] = r.Looked
		stray[r.Key] = r
	}

	liveKeys := make(map[string]bool, len(live))
	for i := range live {
		liveKeys[live[i].Key()] = true
	}

	for k, at := range l.at {
		if !liveKeys[k] {
			continue
		}

		if prev, ok := merged.at[k]; !ok || at.After(prev) {
			merged.at[k] = at
		}
	}

	var f file

	for i := range live {
		c := &live[i]

		at, ok := merged.at[c.Key()]
		if !ok {
			continue
		}

		f.Conversation = append(f.Conversation, record{
			Key: c.Key(), Looked: at, Where: c.Where(), Anchor: c.Anchor(),
		})
	}

	for k, r := range stray {
		if _, seen := liveKeys[k]; seen {
			continue
		}

		if _, ok := merged.at[k]; ok {
			f.Conversation = append(f.Conversation, r)
		}
	}

	return f
}

// Since reports whether you have read the conversation as it now stands. A
// conversation you looked at before its newest comment arrived has not been
// read, which is the case this whole package exists to catch.
func (l *Looked) Since(c *Conversation) bool {
	at, ok := l.at[c.Key()]
	if !ok {
		return false
	}

	return !c.Updated().After(at)
}

// Mark records that the conversation has been read as it now stands.
func (l *Looked) Mark(c *Conversation, now time.Time) { l.at[c.Key()] = now }

// Unmark drops the mark, so the conversation reads as new again. It is the undo
// for reading one by accident while walking the queue.
func (l *Looked) Unmark(c *Conversation) { delete(l.at, c.Key()) }
