package post

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/kyleking/second-look/internal/artifact"
)

// The staged file is deleted once a review posts, which is the right call for
// posting and the wrong one for auditing: nothing on disk then said what was
// sent, when, or what GitHub called it. The record is the audit half — the
// review as it went out plus the id it landed as — under posted/, where a
// staged-review listing never looks.
type record struct {
	Posted postedMeta       `toml:"posted"`
	Review *artifact.Review `toml:"review"`
}

type postedMeta struct {
	// ID is the databaseId of what posted: the review for a whole review, the
	// comment for one sent on its own.
	ID     int64     `toml:"id"`
	NodeID string    `toml:"node_id,omitempty"`
	Kind   string    `toml:"kind"`
	At     time.Time `toml:"at"`
}

// writeRecord keeps the receipt for a post that reached GitHub. It runs before
// the staged file is removed, so a failure is reported while the artifact
// still says everything the record would have.
func writeRecord(root string, r *artifact.Review, id int64, nodeID, kind string) error {
	dir := filepath.Join(root, artifact.Dir, "posted")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	body, err := toml.Marshal(record{
		Posted: postedMeta{ID: id, NodeID: nodeID, Kind: kind, At: time.Now().UTC()},
		Review: r,
	})
	if err != nil {
		return fmt.Errorf("encoding the record: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("pr-%d-%d.toml", r.Number, id))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
