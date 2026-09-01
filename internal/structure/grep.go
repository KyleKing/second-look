package structure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Rule ids. They come back on every match, which is what makes one pass over a
// fragment answer three questions.
const (
	ruleComment = "comment"
	ruleDecl    = "decl"
	ruleCall    = "call"
)

// match is one node the scan found, with the byte range it covers so a caller
// can cut it out of the source it was found in.
type match struct {
	Rule       string
	Start, End int
	Text       string
}

// scan reads one fragment and reports its comments, declarations, and calls.
//
// One subprocess answers all three, because ast-grep takes several rules at
// once and names the one that matched. A fragment is not a file, so the parse
// carries error nodes; tree-sitter recovers around them, which is why a hunk
// starting mid-body still yields the declarations it contains.
func scan(ctx context.Context, l Lang, src string) ([]match, error) {
	if src == "" {
		return nil, nil
	}

	//nolint:gosec // the rules are built from this package's own tables
	cmd := exec.CommandContext(ctx, grepBin, "scan",
		"--inline-rules", rules(l), "--json=compact", "--stdin")
	cmd.Stdin = strings.NewReader(src)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading the %s fragment: %w", l.Name, reason(err))
	}

	return decode(out)
}

// rules is the three-document rule set for one language, which is the whole
// query this package makes.
func rules(l Lang) string {
	docs := []string{doc(ruleComment, l.Name, []string{"comment"})}

	if len(l.Decls) > 0 {
		docs = append(docs, doc(ruleDecl, l.Name, l.Decls))
	}

	if len(l.Calls) > 0 {
		docs = append(docs, doc(ruleCall, l.Name, l.Calls))
	}

	return strings.Join(docs, "\n---\n")
}

func doc(id, lang string, kinds []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "id: %s\nlanguage: %s\nrule:\n  any:\n", id, lang)

	for _, k := range kinds {
		fmt.Fprintf(&b, "    - kind: %s\n", k)
	}

	return b.String()
}

// response is the shape ast-grep prints, kept to the fields a caller reads. The
// names are the tool's own, so they do not follow this project's convention.
//
//nolint:tagliatelle // ast-grep chose these names
type response struct {
	RuleID string `json:"ruleId"`
	Text   string `json:"text"`
	Range  struct {
		ByteOffset struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"byteOffset"`
	} `json:"range"`
}

func decode(body []byte) ([]match, error) {
	var raw []response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("reading the scan results: %w", err)
	}

	out := make([]match, 0, len(raw))
	for i := range raw {
		out = append(out, match{
			Rule:  raw[i].RuleID,
			Start: raw[i].Range.ByteOffset.Start,
			End:   raw[i].Range.ByteOffset.End,
			Text:  raw[i].Text,
		})
	}

	return out, nil
}

// reason puts the tool's own stderr in the error. The exit status alone says
// nothing, and a bad rule and an unknown language fail the same way.
func reason(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return err
}
