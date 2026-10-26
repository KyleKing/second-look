package conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/kyleking/second-look/internal/ghrun"
)

// MaxAge is how old a Record may grow before the next read fetches it again,
// which bounds how long a change the pull request's updatedAt does not carry,
// such as a thread resolved elsewhere, stays wrong.
const MaxAge = 10 * time.Minute

// chunk is how many pull requests one heavy query asks about. GitHub's time on
// a query grows with its nodes, so a read of forty pull requests in four
// queries at once finishes in about half the time of one.
const chunk = 10

// Candidate is a pull request the light read found: enough to tell whether the
// conversations held for it are still current.
type Candidate struct {
	ID         string
	Repository string
	Number     int
	Updated    time.Time
	Head       string
}

func (c Candidate) key() string { return KeyOf(c.Repository, c.Number) }

// KeyOf is how a Record is keyed: the pull request as owner/repo#n.
func KeyOf(repository string, number int) string {
	return repository + "#" + strconv.Itoa(number)
}

// Record is what a read knew of one pull request when it fetched its
// conversations. A pull request holding none still has one, so it is not
// fetched again for being empty.
type Record struct {
	Updated time.Time `json:"updated"`
	Head    string    `json:"head"`
	Fetched time.Time `json:"fetched"`
}

// Prior is what an earlier read left behind: the queue and what each pull
// request in it was read as.
type Prior struct {
	Queue   Queue
	Records map[string]Record
}

// Read is one read of the queue: the conversations, the record of each pull
// request they came from, and how many pull requests were fetched rather than
// reused.
type Read struct {
	Queue   *Queue
	Records map[string]Record
	Fetched int
	Reused  int
}

// Split divides candidates into those whose Record still holds and those a
// heavy read must fetch: one nobody has read, one that moved or changed head
// since, and one read longer ago than MaxAge. A pull request appears once
// however many times the search found it.
func Split(records map[string]Record, cands []Candidate, now time.Time) ([]Candidate, []Candidate) {
	var reuse, fetch []Candidate

	seen := make(map[string]bool, len(cands))

	for _, c := range cands {
		if seen[c.key()] {
			continue
		}

		seen[c.key()] = true

		rec, held := records[c.key()]

		switch {
		case !held, !rec.Updated.Equal(c.Updated), rec.Head != c.Head, now.Sub(rec.Fetched) > MaxAge:
			fetch = append(fetch, c)
		default:
			reuse = append(reuse, c)
		}
	}

	return reuse, fetch
}

// LightArgs is the gh call that finds the candidate pull requests, exported for
// the reason Decode is: a replay builds its interaction from this rather than
// from a copy of the query.
func LightArgs(limit int) []string {
	if limit <= 0 {
		limit = DefaultLimit
	}

	return []string{
		"api", "graphql",
		"-F", "q=" + Query,
		"-F", "n=" + strconv.Itoa(limit),
		"-f", "query=" + lightQuery,
	}
}

// HeavyArgs is the gh call that reads the conversations of the pull requests
// ids name.
func HeavyArgs(ids []string) []string {
	args := make([]string, 0, 4+2*len(ids))
	args = append(args, "api", "graphql", "-f", "query="+heavyQuery)

	for _, id := range ids {
		args = append(args, "-f", "nodes[]="+id)
	}

	return args
}

// Chunks is how a set of ids is divided into heavy reads, exported so a replay
// builds the same calls the fetcher makes.
func Chunks(ids []string) [][]string { return slices.Collect(slices.Chunk(ids, chunk)) }

// Fetch reads every qualifying conversation, with nothing held from before.
func Fetch(ctx context.Context, root string, limit int) (*Queue, error) {
	r, err := ReadQueue(ctx, root, limit, nil, time.Now())
	if err != nil {
		return nil, err
	}

	return r.Queue, nil
}

// ReadQueue finds the candidate pull requests in one cheap query, then fetches
// the conversations of only those prior cannot vouch for, in parallel chunks.
// With no prior, or one read as somebody else, every pull request is fetched.
func ReadQueue(ctx context.Context, root string, limit int, prior *Prior, now time.Time) (*Read, error) {
	viewer, cands, err := light(ctx, root, limit)
	if err != nil {
		return nil, err
	}

	if prior == nil || prior.Queue.Viewer != viewer {
		prior = &Prior{}
	}

	reuse, fetch := Split(prior.Records, cands, now)

	ids := make([]string, 0, len(fetch))
	for _, c := range fetch {
		ids = append(ids, c.ID)
	}

	fresh, err := heavy(ctx, root, ids)
	if err != nil {
		return nil, err
	}

	return assemble(viewer, prior, cands, reuse, fresh, now), nil
}

func assemble(
	viewer string, prior *Prior, cands, reuse []Candidate, fresh map[string][]Conversation, now time.Time,
) *Read {
	kept := make(map[string][]Conversation, len(prior.Queue.Conversations))

	for i := range prior.Queue.Conversations {
		c := &prior.Queue.Conversations[i]
		kept[KeyOf(c.Repository, c.Number)] = append(kept[KeyOf(c.Repository, c.Number)], *c)
	}

	reused := make(map[string]bool, len(reuse))
	for _, c := range reuse {
		reused[c.key()] = true
	}

	read := &Read{
		Queue:   &Queue{Viewer: viewer},
		Records: make(map[string]Record, len(cands)),
		Reused:  len(reuse),
	}

	seen := make(map[string]bool, len(cands))

	for _, c := range cands {
		key := c.key()
		if seen[key] {
			continue
		}

		seen[key] = true

		if convs, ok := fresh[key]; ok {
			read.Queue.Conversations = append(read.Queue.Conversations, convs...)
			read.Records[key] = Record{Updated: c.Updated, Head: c.Head, Fetched: now}
			read.Fetched++

			continue
		}

		if reused[key] {
			read.Queue.Conversations = append(read.Queue.Conversations, kept[key]...)
			read.Records[key] = prior.Records[key]
		}
	}

	return read
}

func light(ctx context.Context, root string, limit int) (string, []Candidate, error) {
	out, err := runGH(ctx, root, LightArgs(limit))
	if err != nil {
		return "", nil, fmt.Errorf("reading your open conversations: %w", err)
	}

	var r lightAnswer
	if err := json.Unmarshal(out, &r); err != nil {
		return "", nil, fmt.Errorf("reading your open conversations: %w", err)
	}

	if r.Data.Viewer.Login == "" {
		return "", nil, ErrNoViewer
	}

	cands := make([]Candidate, 0, len(r.Data.Search.Nodes))

	for _, n := range r.Data.Search.Nodes {
		if n.Number == 0 {
			continue
		}

		cands = append(cands, Candidate{
			ID: n.ID, Repository: n.Repository.NameWithOwner, Number: n.Number,
			Updated: n.UpdatedAt, Head: n.HeadRefOid,
		})
	}

	return r.Data.Viewer.Login, cands, nil
}

// heavy reads the conversations of ids in parallel chunks, keyed by pull
// request. A pull request the forge answered for and found nothing of yours in
// is present with none.
func heavy(ctx context.Context, root string, ids []string) (map[string][]Conversation, error) {
	out := make(map[string][]Conversation, len(ids))

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)

	for _, part := range Chunks(ids) {
		wg.Add(1)

		go func() {
			defer wg.Done()

			pulls, err := heavyChunk(ctx, root, part)

			mu.Lock()
			defer mu.Unlock()

			if err != nil && firstErr == nil {
				firstErr = err
			}

			for _, p := range pulls {
				out[p.key] = p.conversations
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return out, nil
}

func heavyChunk(ctx context.Context, root string, ids []string) ([]pulled, error) {
	body, err := runGH(ctx, root, HeavyArgs(ids))
	if err != nil {
		return nil, fmt.Errorf("reading %d pull requests' conversations: %w", len(ids), err)
	}

	_, pulls, err := decodePulls(body)

	return pulls, err
}

func runGH(ctx context.Context, root string, args []string) ([]byte, error) {
	release, err := ghrun.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting gh: %w", err)
	}

	defer release()

	//nolint:gosec // the query is a constant and the ids are the forge's own
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, ghError(err)
	}

	return out, nil
}
