package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Ratings are what earlier runs made of the pull requests a queue lists. They
// live in one file under the state home rather than one per repository,
// because a queue spans repositories and a row nobody has staged a review for
// has no artifact tree of its own to keep a rating in.
//
// Nothing here is authoritative. An entry whose update time is not the one the
// search reported is re-rated, and a file that cannot be read is no ratings
// rather than an error: a queue orders itself worse without them and works.
type Ratings map[string]Rating

// Rating is what one pull request's diff was made of and the update time it was
// read at. The time is the invalidation: a push moves it, so a rating recorded
// against the old one is about a diff that no longer stands.
type Rating struct {
	Updated time.Time `toml:"updated"`
	Cost    int       `toml:"cost"`
	// Rated is false for a diff no grammar answered for, which is a lockfile
	// bump or a directory of YAML. It is recorded all the same, so the queue
	// asks once per push rather than fetching the same unratable diff on every
	// open.
	Rated bool `toml:"rated"`
}

// RatingKey names a pull request the way a queue row does.
func RatingKey(repository string, number int) string {
	return fmt.Sprintf("%s#%d", repository, number)
}

// RatingsPath is where the queue's ratings are kept.
func RatingsPath() (string, error) {
	home, err := StateHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "ratings.toml"), nil
}

// RatingScale is what the numbers in the cache are on. Changing how a cost is
// worked out changes what every cached number means, and a queue ordering a mix
// of two scales orders wrongly and silently, so a file written on another one
// is dropped rather than read.
const RatingScale = 2

// ratingFile is the on-disk shape. The key is a field rather than a table name
// because a repository name carries characters a bare TOML key cannot.
type ratingFile struct {
	Scale int        `toml:"scale"`
	Rated []ratedRow `toml:"rated"`
}

type ratedRow struct {
	Key     string    `toml:"key"`
	Updated time.Time `toml:"updated"`
	Cost    int       `toml:"cost"`
	Rated   bool      `toml:"rated"`
}

// LoadRatings reads what earlier runs rated. Every failure answers with no
// ratings: the queue falls back to ordering by age, which is what it did before
// anything was rated at all.
func LoadRatings() Ratings {
	path, err := RatingsPath()
	if err != nil {
		return Ratings{}
	}

	body, err := os.ReadFile(path) //nolint:gosec // the state home plus a constant name
	if err != nil {
		return Ratings{}
	}

	var file ratingFile
	if err := toml.NewDecoder(strings.NewReader(string(body))).Decode(&file); err != nil {
		return Ratings{}
	}

	if file.Scale != RatingScale {
		return Ratings{}
	}

	out := make(Ratings, len(file.Rated))
	for _, r := range file.Rated {
		out[r.Key] = Rating{Updated: r.Updated, Cost: r.Cost, Rated: r.Rated}
	}

	return out
}

// SaveRatings replaces the file with what the caller holds, so a queue writing
// back what it just listed drops the entries for pull requests that have left
// it. The file is a cache and bounding it costs nothing else.
func SaveRatings(r Ratings) error {
	path, err := RatingsPath()
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	file := ratingFile{Scale: RatingScale, Rated: make([]ratedRow, 0, len(keys))}
	for _, k := range keys {
		file.Rated = append(file.Rated,
			ratedRow{Key: k, Updated: r[k].Updated, Cost: r[k].Cost, Rated: r[k].Rated})
	}

	body, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encoding the ratings: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
