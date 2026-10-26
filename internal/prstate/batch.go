package prstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/kyleking/second-look/internal/ghrun"
)

// MaxAliases is how many pull requests one batched query asks about.
const MaxAliases = 50

// parts is how many queries a read is split across, which run side by side.
const parts = 4

// Split divides refs into the queries FetchAll sends: at most parts of them,
// each no larger than MaxAliases, so the forge's time on one query (which
// grows with its aliases) is spent in parallel.
func Split(refs []Ref) [][]Ref {
	if len(refs) == 0 {
		return nil
	}

	size := min(MaxAliases, (len(refs)+parts-1)/parts)

	return slices.Collect(slices.Chunk(refs, size))
}

// Ref names one pull request.
type Ref struct {
	Repository string
	Number     int
}

// BatchArgs is the gh call that reads every ref in one query, exported so a
// test's cassette is built from it rather than from a copy.
func BatchArgs(refs []Ref) ([]string, error) {
	var decl, body strings.Builder

	args := []string{"api", "graphql"}

	for i, r := range refs {
		owner, name, ok := strings.Cut(r.Repository, "/")
		if !ok {
			return nil, fmt.Errorf("%q: %w", r.Repository, ErrNotARepository)
		}

		fmt.Fprintf(&decl, "$o%[1]d:String!,$n%[1]d:String!,$p%[1]d:Int!,", i)
		fmt.Fprintf(&body,
			"r%[1]d:repository(owner:$o%[1]d,name:$n%[1]d){pullRequest(number:$p%[1]d){%[2]s}}\n", i, fields)

		args = append(args,
			"-f", fmt.Sprintf("o%d=%s", i, owner), "-f", fmt.Sprintf("n%d=%s", i, name),
			"-F", fmt.Sprintf("p%d=%s", i, strconv.Itoa(r.Number)))
	}

	query := "query(" + strings.TrimSuffix(decl.String(), ",") + "){\n" + body.String() + "}"

	return append(args, "-f", "query="+query), nil
}

// FetchAll reads every ref's state, several queries run in parallel. A ref the
// forge did not answer for is absent from the result, and the error names why
// the last failed query did.
func FetchAll(ctx context.Context, root string, refs []Ref) (map[Ref]State, error) {
	out := make(map[Ref]State, len(refs))

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)

	for _, chunk := range Split(refs) {
		wg.Add(1)

		go func() {
			defer wg.Done()

			got, err := fetchChunk(ctx, root, chunk)

			mu.Lock()
			defer mu.Unlock()

			for ref, state := range got {
				out[ref] = state
			}

			if err != nil && firstErr == nil {
				firstErr = err
			}
		}()
	}

	wg.Wait()

	return out, firstErr
}

func fetchChunk(ctx context.Context, root string, refs []Ref) (map[Ref]State, error) {
	args, err := BatchArgs(refs)
	if err != nil {
		return nil, err
	}

	release, err := ghrun.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting gh: %w", err)
	}

	defer release()

	//nolint:gosec // every argument is a constant or a value read off the pull request
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root

	// gh exits non-zero when any alias errored and still prints the others.
	body, runErr := cmd.Output()

	states, decodeErr := DecodeBatch(body, refs)
	if len(states) > 0 {
		return states, nil
	}

	if runErr != nil {
		return nil, fmt.Errorf("reading the state of %d pull requests: %w", len(refs), runErr)
	}

	return states, decodeErr
}

// DecodeBatch reads the answer to BatchArgs for refs.
func DecodeBatch(body []byte, refs []Ref) (map[Ref]State, error) {
	var reply struct {
		Data map[string]json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, fmt.Errorf("reading the pull requests' states: %w", err)
	}

	out := make(map[Ref]State, len(refs))

	for i, ref := range refs {
		raw, ok := reply.Data["r"+strconv.Itoa(i)]
		if !ok || string(raw) == "null" {
			continue
		}

		state, err := Decode([]byte(`{"data":{"repository":` + string(raw) + `}}`))
		if err != nil {
			return out, err
		}

		out[ref] = state
	}

	return out, nil
}
