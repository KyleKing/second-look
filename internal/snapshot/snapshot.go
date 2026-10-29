// Package snapshot keeps the last queue each tab drew, so opening the program
// paints it at once and refreshes behind it.
//
// It holds private repository names, titles, and review text. It writes through
// aragonite's disk cache, which keeps its directory at 0700 and its files at
// 0600 and writes them atomically. A snapshot is never a fixture: this
// repository is public.
package snapshot

import (
	"maps"
	"slices"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/notice"
	"github.com/kyleking/second-look/internal/prstate"
)

const (
	upstream = "queue"
	// The memory TTL only decides what counts as recent. Disk keeps an entry for
	// aragonite's own seven days of stale retention.
	memoryTTL = 24 * time.Hour
	// The schema is bumped whenever a record gains, loses, or renames a field: a
	// record written under another version reads as absent.
	schema = 2

	keyInbox         = "inbox"
	keyConversations = "conversations"
	keyStaged        = "staged"
)

// Header is what every record carries: the schema it was written under and the
// login it was read as.
type Header struct {
	Version int    `json:"version"`
	Viewer  string `json:"viewer"`
}

func (h Header) header() Header { return h }

// Owned reports whether the record was read as viewer. A record of another
// account is wrong rather than old.
func (h Header) Owned(viewer string) bool { return h.Viewer == viewer }

// Row is a search result with the node id that PullRequest keeps out of its
// JSON, since the id is what a facts read asks by.
type Row struct {
	ID         string    `json:"id"`
	Created    time.Time `json:"created"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	Draft      bool      `json:"draft"`
	Comments   int       `json:"comments"`
	Labels     []string  `json:"labels"`
	Updated    time.Time `json:"updated"`
	URL        string    `json:"url"`
}

// RowsOf is the records of a bucket's items.
func RowsOf(items []inbox.PullRequest) []Row {
	rows := make([]Row, 0, len(items))

	for i := range items {
		p := &items[i]
		rows = append(rows, Row{
			ID: p.ID, Created: p.Created, Repository: p.Repository, Number: p.Number,
			Title: p.Title, Author: p.Author, Draft: p.Draft, Comments: p.Comments,
			Labels: p.Labels, Updated: p.Updated, URL: p.URL,
		})
	}

	return rows
}

// PullRequests is the items a record's rows stand for.
func PullRequests(rows []Row) []inbox.PullRequest {
	items := make([]inbox.PullRequest, 0, len(rows))

	for i := range rows {
		r := &rows[i]
		items = append(items, inbox.PullRequest{
			ID: r.ID, Created: r.Created, Repository: r.Repository, Number: r.Number,
			Title: r.Title, Author: r.Author, Draft: r.Draft, Comments: r.Comments,
			Labels: r.Labels, Updated: r.Updated, URL: r.URL,
		})
	}

	return items
}

// Bucket is one section of the inbox as its search answered.
type Bucket struct {
	Name  string `json:"name"`
	Items []Row  `json:"items"`
}

// Inbox is the inbox tab: every bucket's rows and the facts read for them.
type Inbox struct {
	Header
	Buckets []Bucket                `json:"buckets"`
	Facts   map[string]inbox.Record `json:"facts"`
	// Verified says this process read the snapshot's viewer from the forge. It
	// is never written to disk, so a snapshot loaded from disk is unverified.
	Verified bool `json:"-"`
}

// Conversations is the conversation tab: the queue, what each pull request in
// it was read as, and the notification stream. Denied is when GitHub last
// refused the stream to this token, so the next open does not ask again within
// DeniedFor.
type Conversations struct {
	Header
	Queue   conversations.Queue             `json:"queue"`
	Records map[string]conversations.Record `json:"records"`
	Notices []notice.Thread                 `json:"notices"`
	Denied  time.Time                       `json:"denied,omitzero"`
}

// DeniedFor is how long a refused notification stream is remembered.
const DeniedFor = 24 * time.Hour

// Refused reports a stream refused recently enough that asking again is wasted.
func (c Conversations) Refused(now time.Time) bool {
	return !c.Denied.IsZero() && now.Sub(c.Denied) < DeniedFor
}

// Staged is the staged tab: what the forge said of each review, keyed like the
// rows.
type Staged struct {
	Header
	States map[string]prstate.State `json:"states"`
}

// Store reads and writes the three snapshots through one disk cache.
type Store struct {
	inbox         slot[Inbox]
	conversations slot[Conversations]
	staged        slot[Staged]
}

// Open returns a store writing through disk, which may be nil to keep
// snapshots in memory for this run alone. The clock is what ages are read from.
func Open(disk *cache.DiskCache, now func() time.Time) *Store {
	return &Store{
		inbox:         newSlot[Inbox](disk, keyInbox, now),
		conversations: newSlot[Conversations](disk, keyConversations, now),
		staged:        newSlot[Staged](disk, keyStaged, now),
	}
}

// Inbox is the last inbox snapshot and how long ago it was saved.
func (s *Store) Inbox() (Inbox, time.Duration, bool) { return s.inbox.load() }

// SaveInbox records v as read by viewer.
func (s *Store) SaveInbox(viewer string, v Inbox) {
	v.Header = Header{Version: schema, Viewer: viewer}
	s.inbox.save(v)
}

// Evict forgets a pull request the reader acted on: its row and facts in the
// inbox, and its conversations and record in the conversation queue. The next
// open then paints no row for it, and whatever the read finds is drawn as new
// rather than as it stood before the action.
func (s *Store) Evict(repository string, number int) {
	key := conversations.KeyOf(repository, number)

	isIt := func(r Row) bool { return r.Repository == repository && r.Number == number }
	isItsConversation := func(c conversations.Conversation) bool {
		return c.Repository == repository && c.Number == number
	}

	if v, _, ok := s.inbox.load(); ok {
		for i := range v.Buckets {
			v.Buckets[i].Items = slices.DeleteFunc(slices.Clone(v.Buckets[i].Items), isIt)
		}

		v.Facts = maps.Clone(v.Facts)
		delete(v.Facts, key)

		s.SaveInbox(v.Viewer, v)
	}

	if v, _, ok := s.conversations.load(); ok {
		v.Queue.Conversations = slices.DeleteFunc(slices.Clone(v.Queue.Conversations), isItsConversation)

		v.Records = maps.Clone(v.Records)
		delete(v.Records, key)

		s.SaveConversations(v)
	}
}

// DropInbox makes the inbox snapshot read as absent, in memory and on disk.
func (s *Store) DropInbox() { s.inbox.drop() }

// Conversations is the last conversation snapshot and how long ago it was saved.
func (s *Store) Conversations() (Conversations, time.Duration, bool) { return s.conversations.load() }

// SaveConversations records v as read by the viewer the queue names.
func (s *Store) SaveConversations(v Conversations) {
	v.Header = Header{Version: schema, Viewer: v.Queue.Viewer}
	s.conversations.save(v)
}

// DropConversations makes the conversation snapshot read as absent.
func (s *Store) DropConversations() { s.conversations.drop() }

// Staged is the last staged-states snapshot and how long ago it was saved.
func (s *Store) Staged() (Staged, time.Duration, bool) { return s.staged.load() }

// SaveStaged records v as read by viewer.
func (s *Store) SaveStaged(viewer string, v Staged) {
	v.Header = Header{Version: schema, Viewer: viewer}
	s.staged.save(v)
}

// DropStaged makes the staged snapshot read as absent.
func (s *Store) DropStaged() { s.staged.drop() }

// slot is one record's snapshot: memory over disk, with the process's clock
// injectable so a test can age a record without sleeping.
type slot[T interface{ header() Header }] struct {
	snap *cache.Snapshot[T]
}

func newSlot[T interface{ header() Header }](disk *cache.DiskCache, key string, now func() time.Time) slot[T] {
	return slot[T]{snap: cache.NewSnapshot(cache.NewTTLCacheWithClock[T](memoryTTL, now), disk, upstream, key)}
}

//nolint:ireturn // T is the record the slot was made for
func (s slot[T]) load() (T, time.Duration, bool) {
	v, age, ok := s.snap.Load(cache.NoStamp)
	if !ok || v.header().Version != schema {
		var zero T

		return zero, 0, false
	}

	return v, age, true
}

func (s slot[T]) save(v T) { s.snap.Save(cache.NoStamp, v) }

func (s slot[T]) drop() { s.snap.Drop() }
