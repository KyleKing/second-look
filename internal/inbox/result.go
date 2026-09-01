package inbox

import "time"

// result is gh search's answer, shaped only where this package reads it.
//
//nolint:tagliatelle // gh answers in camelCase and these names are its own
type result struct {
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	IsDraft       bool `json:"isDraft"`
	CommentsCount int  `json:"commentsCount"`
	Labels        []struct {
		Name string `json:"name"`
	} `json:"labels"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

func (r *result) pullRequest() PullRequest {
	labels := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, l.Name)
	}

	return PullRequest{
		Repository: r.Repository.NameWithOwner,
		Number:     r.Number,
		Title:      r.Title,
		Author:     r.Author.Login,
		Draft:      r.IsDraft,
		Comments:   r.CommentsCount,
		Labels:     labels,
		Updated:    r.UpdatedAt,
		URL:        r.URL,
	}
}
