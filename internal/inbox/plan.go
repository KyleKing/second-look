package inbox

import "time"

// MaxAge is how old a Record may grow before the next read fetches it again,
// which bounds how long a change the search's updatedAt does not carry stays
// wrong.
const MaxAge = 10 * time.Minute

// Split divides rows into those whose Record still holds and those a facts read
// must fetch: a row nobody has read, one whose updatedAt moved since, one whose
// checks were still running, and one read longer ago than MaxAge. A row appears
// once however many searches found it, and a row with no node id cannot be
// asked about, so it is reused as it stands. Passing no records fetches every
// row.
func Split(records map[string]Record, rows []PullRequest, now time.Time) ([]PullRequest, []PullRequest) {
	var reuse, fetch []PullRequest

	seen := make(map[string]bool, len(rows))

	for i := range rows {
		p := &rows[i]
		key := keyOf(p)

		if seen[key] {
			continue
		}

		seen[key] = true

		rec, held := records[key]

		switch {
		case p.ID == "":
			reuse = append(reuse, *p)
		case !held, !rec.Updated.Equal(p.Updated), rec.Facts.running(), now.Sub(rec.Fetched) > MaxAge:
			fetch = append(fetch, *p)
		default:
			reuse = append(reuse, *p)
		}
	}

	return reuse, fetch
}

// running reports checks that have not finished. It exists because a check suite
// ending may not move updatedAt; retire it once a probe on a pull request with a
// running check shows that it does.
func (f *Facts) running() bool { return f.Checks == "PENDING" || f.Checks == "EXPECTED" }
