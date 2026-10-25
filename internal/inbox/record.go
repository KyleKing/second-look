package inbox

import "time"

// Record is one pull request's Facts as last read, with what says whether they
// can still be trusted: the search's updatedAt when they were read, and when.
type Record struct {
	Facts   Facts     `json:"facts"`
	Updated time.Time `json:"updated"`
	Fetched time.Time `json:"fetched"`
}
