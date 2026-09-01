package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	errNotAPRRef = errors.New("not a pull request: give a number, owner/repo#number, or a pull request URL")
	errNotAPRURL = errors.New("not a pull request URL")
)

// ref is a pull request named on the command line.
//
// A bare number means this checkout's own repository, which is what most of a
// day is. Any other is named as owner/repo#number or by its URL, which is what
// makes a review possible from outside a clone of it.
type ref struct {
	owner  string
	repo   string
	number int
}

// here reports a reference that names no repository, so the checkout's own is
// the one meant.
func (r ref) here() bool { return r.owner == "" }

func (r ref) String() string {
	if r.here() {
		return "#" + strconv.Itoa(r.number)
	}

	return fmt.Sprintf("%s/%s#%d", r.owner, r.repo, r.number)
}

// parseRef reads the three shapes. A URL parses because that is what a browser
// and a pull request comment both hand over, and retyping it as owner/repo#n is
// a transcription nobody should make.
func parseRef(s string) (ref, error) {
	s = strings.TrimSpace(s)

	if strings.Contains(s, "://") {
		return parseURL(s)
	}

	repo, number, found := strings.Cut(s, "#")
	if !found {
		repo, number = "", s
	}

	n, err := prNumber(number)
	if err != nil {
		return ref{}, fmt.Errorf("%q is %w", s, errNotAPRRef)
	}

	if repo == "" {
		return ref{number: n}, nil
	}

	owner, name, found := strings.Cut(repo, "/")
	if !found || owner == "" || name == "" || strings.Contains(name, "/") {
		return ref{}, fmt.Errorf("%q is %w", s, errNotAPRRef)
	}

	return ref{owner: owner, repo: name, number: n}, nil
}

// parseURL reads github.com/owner/repo/pull/42, ignoring whatever a browser
// appended to it: a review comment's fragment and a diff's query are both part
// of a link somebody copied.
func parseURL(s string) (ref, error) {
	rest := s
	if _, after, found := strings.Cut(s, "://"); found {
		rest = after
	}

	rest, _, _ = strings.Cut(rest, "?")
	rest, _, _ = strings.Cut(rest, "#")

	parts := strings.Split(strings.Trim(rest, "/"), "/")

	const want = 5
	if len(parts) < want || parts[3] != "pull" {
		return ref{}, fmt.Errorf("%q is %w", s, errNotAPRURL)
	}

	n, err := prNumber(parts[4])
	if err != nil {
		return ref{}, fmt.Errorf("%q is %w", s, errNotAPRURL)
	}

	return ref{owner: parts[1], repo: parts[2], number: n}, nil
}
