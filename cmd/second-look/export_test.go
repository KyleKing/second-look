package main

import "github.com/kyleking/second-look/internal/prepared"

// RefString parses a pull request reference and renders it back, which is both
// halves of the parser without exporting its type.
func RefString(s string) (string, error) {
	r, err := parseRef(s)
	if err != nil {
		return "", err
	}

	return r.String(), nil
}

// StagedRow is what one staged review's row says it holds, and whether C would
// act on it, for a directory standing at head in repo.
func StagedRow(review prepared.Review, repo, head string) (string, bool) {
	s := &reviewsScreen{here: repo, head: head, rows: []prepared.Review{review}}

	_, _, err := s.checkout(review.Where())

	return s.tail(&s.rows[0]), err == nil
}
