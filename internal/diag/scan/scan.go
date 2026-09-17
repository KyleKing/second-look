// Package scan runs the checkers a person keeps outside their repository's CI.
//
// A review is where a rule nobody's CI enforces still has to be applied: a
// house rule about comment length, a ruff selector the project has not adopted,
// a linter run at a strictness the build cannot afford. Each of those is a
// command that already exists, so this runs the ones the config names against
// the checkout and reads their answers back.
//
// Nothing here knows a rule. A command and the shape it prints in is the whole
// of what a check is, which is what keeps a rule the user invented as first
// class as one this package had heard of.
package scan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diag/project"
)

// Format is how a checker prints what it found.
type Format string

// The formats read here. Text is the one nearly every linter can be asked for,
// and the other two are read natively because their JSON carries a rule name
// and a severity that a line of text has thrown away.
const (
	AstGrep Format = "ast-grep"
	Ruff    Format = "ruff"
	Text    Format = "text"
)

// Reasons a check is refused.
var (
	// ErrFormat reports a check naming a shape nothing here can read.
	ErrFormat = errors.New("unknown check format")
	// ErrNoCommand reports a check with nothing to run.
	ErrNoCommand = errors.New("a check needs a command")
)

// Check is one configured checker.
type Check struct {
	// Name is what its notes are attributed to.
	Name string
	// Command is argv. A {files} argument is replaced by the paths under
	// review; a command without one is run as written and its answers are
	// filtered against the diff afterwards.
	Command []string
	Format  Format
	// Roots are the files marking the project the check is run in, believed in
	// the order they are written. Unset, the check runs once at the checkout,
	// which in a monorepo applies one package's settings to every package.
	Roots [][]string
	// Bin are directories under a project root holding the project's own copy
	// of the tool, searched before the PATH. A linter from the PATH is a
	// different version under different settings than the one the project pins,
	// and the two disagree about the same file.
	Bin []string
}

// filesToken is the placeholder a command uses to be handed the review's files.
const filesToken = "{files}"

// Run runs every check against the checkout and returns what they found.
//
// A check that fails is reported and does not stop the others: a linter missing
// from a laptop is worth one line in a footer, and is not worth losing the
// checks that did run.
func Run(ctx context.Context, checkout string, checks []Check, files []string) ([]diag.Note, error) {
	var (
		out  []diag.Note
		errs []error
	)

	for _, c := range checks {
		for _, at := range byProject(checkout, c, files) {
			notes, err := one(ctx, checkout, at.root, c, at.files)
			if err != nil {
				errs = append(errs, err)

				continue
			}

			out = append(out, notes...)
		}
	}

	return out, errors.Join(errs...)
}

// group is the files of one check that belong to one project.
type group struct {
	root  string
	files []string
}

// byProject splits a check's files by the project each belongs to, in path
// order so a run reports the same thing twice. A check naming no markers is one
// group at the checkout, which is what a single-project repository wants.
func byProject(checkout string, c Check, files []string) []group {
	if len(c.Roots) == 0 {
		return []group{{root: checkout, files: files}}
	}

	at := map[string][]string{}
	for _, f := range files {
		root := project.Root(checkout, f, c.Roots, nil)
		at[root] = append(at[root], f)
	}

	out := make([]group, 0, len(at))
	for root, held := range at {
		out = append(out, group{root: root, files: held})
	}

	slices.SortFunc(out, func(a, b group) int { return strings.Compare(a.root, b.root) })

	return out
}

func one(ctx context.Context, checkout, root string, c Check, files []string) ([]diag.Note, error) {
	if len(c.Command) == 0 {
		return nil, fmt.Errorf("check %q: %w", c.Name, ErrNoCommand)
	}

	argv := expand(c.Command, under(checkout, root, files))
	if argv == nil {
		// Every file under review was filtered out of the command, so there is
		// nothing to point it at. Running it anyway would check the project.
		return nil, nil
	}

	argv[0] = project.Tool(checkout, root, argv[0], c.Bin)

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- a configured check command
	cmd.Dir = root

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	// A checker that found something exits non-zero, which is the same status a
	// checker that could not run exits with. So output decides: anything this
	// can read is an answer, and a failure with nothing readable is an error.
	stdout, runErr := cmd.Output()

	notes, err := Read(c.Format, c.Name, checkout, root, stdout)
	if err == nil && (len(notes) > 0 || runErr == nil) {
		return notes, nil
	}

	if runErr != nil {
		return nil, fmt.Errorf("check %q: %w: %s",
			c.Name, runErr, strings.TrimSpace(stderr.String()))
	}

	return nil, fmt.Errorf("check %q: %w", c.Name, err)
}

// under is the review's files as the directory the check runs in spells them.
func under(checkout, root string, files []string) []string {
	if checkout == root {
		return files
	}

	out := make([]string, 0, len(files))

	for _, f := range files {
		at, err := filepath.Rel(root, filepath.Join(checkout, f))
		if err != nil {
			at = f
		}

		out = append(out, filepath.ToSlash(at))
	}

	return out
}

// expand replaces the files placeholder, and reports nil where the command
// wanted files and none of the review's are left.
func expand(argv, files []string) []string {
	if !slices.Contains(argv, filesToken) {
		return argv
	}

	if len(files) == 0 {
		return nil
	}

	out := make([]string, 0, len(argv)+len(files))

	for _, a := range argv {
		if a == filesToken {
			out = append(out, files...)

			continue
		}

		out = append(out, a)
	}

	return out
}

// Read parses one checker's output.
func Read(f Format, name, checkout, root string, out []byte) ([]diag.Note, error) {
	where := paths{checkout: checkout, root: root}

	switch f {
	case AstGrep:
		return readAstGrep(name, where, out)
	case Ruff:
		return readRuff(name, where, out)
	case Text, "":
		return readText(name, where, out), nil
	}

	return nil, fmt.Errorf("%w: %q", ErrFormat, f)
}

// astGrepMatch is one match, whose lines are counted from zero.
type astGrepMatch struct {
	File  string `json:"file"`
	Range struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
		End struct {
			Line int `json:"line"`
		} `json:"end"`
	} `json:"range"`
	RuleID   string `json:"ruleId"` //nolint:tagliatelle // ast-grep's own spelling
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func readAstGrep(name string, where paths, out []byte) ([]diag.Note, error) {
	var ms []astGrepMatch
	if err := json.Unmarshal(bytes.TrimSpace(out), &ms); err != nil {
		return nil, fmt.Errorf("reading ast-grep's answer: %w", err)
	}

	notes := make([]diag.Note, 0, len(ms))

	for _, m := range ms {
		notes = append(notes, diag.Note{
			Path:     where.rel(m.File),
			Line:     m.Range.Start.Line + 1,
			End:      m.Range.End.Line + 1,
			Source:   name,
			Code:     m.RuleID,
			Message:  strings.TrimSpace(m.Message),
			Severity: severityWord(m.Severity),
		})
	}

	return notes, nil
}

type ruffMatch struct {
	Filename string `json:"filename"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location struct {
		Row int `json:"row"`
	} `json:"location"`
	EndLocation struct {
		Row int `json:"row"`
	} `json:"end_location"`
}

func readRuff(name string, where paths, out []byte) ([]diag.Note, error) {
	var ms []ruffMatch
	if err := json.Unmarshal(bytes.TrimSpace(out), &ms); err != nil {
		return nil, fmt.Errorf("reading ruff's answer: %w", err)
	}

	notes := make([]diag.Note, 0, len(ms))

	for _, m := range ms {
		notes = append(notes, diag.Note{
			Path:     where.rel(m.Filename),
			Line:     m.Location.Row,
			End:      m.EndLocation.Row,
			Source:   name,
			Code:     m.Code,
			Message:  strings.TrimSpace(m.Message),
			Severity: diag.Warning,
		})
	}

	return notes, nil
}

// textLine is path:line:column: message, with the column optional. It is what
// every linter can be asked to print and what none of them says a severity in,
// so a note read this way is a warning.
func readText(name string, where paths, out []byte) []diag.Note {
	var notes []diag.Note

	for _, line := range strings.Split(string(out), "\n") {
		n, ok := readTextLine(name, where, strings.TrimSpace(line))
		if ok {
			notes = append(notes, n)
		}
	}

	return notes
}

func readTextLine(name string, where paths, line string) (diag.Note, bool) {
	// A Windows path carries a colon of its own, which is why the split counts
	// from the right rather than the left.
	path, rest, ok := cutLast(line)
	if !ok {
		return diag.Note{}, false
	}

	fields := strings.SplitN(rest, ":", 3) //nolint:mnd // line, column, message
	at, err := strconv.Atoi(fields[0])

	if err != nil || at < 1 {
		return diag.Note{}, false
	}

	msg := ""
	if len(fields) > 1 {
		msg = strings.TrimSpace(fields[len(fields)-1])
	}

	return diag.Note{
		Path: where.rel(path), Line: at, Source: name,
		Message: msg, Severity: diag.Warning,
	}, msg != ""
}

// cutLast splits a line into the path and everything after it, which is the
// first colon followed by a digit.
func cutLast(line string) (string, string, bool) {
	for i := 1; i < len(line)-1; i++ {
		if line[i] == ':' && line[i+1] >= '0' && line[i+1] <= '9' {
			return line[:i], line[i+1:], true
		}
	}

	return "", "", false
}

// paths is the checkout the diff is spelled against and the project the check
// ran in, which are the same directory outside a monorepo.
type paths struct {
	checkout string
	root     string
}

// rel is a checker's path as the diff spells it. Some print relative to where
// they ran and some print absolute, and a note whose path does not match the
// diff's is a note that lands nowhere.
func (p paths) rel(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.root, path)
	}

	out, err := filepath.Rel(p.checkout, path)
	if err != nil {
		return filepath.ToSlash(path)
	}

	return filepath.ToSlash(out)
}

func severityWord(s string) diag.Severity {
	switch strings.ToLower(s) {
	case "error":
		return diag.Error
	case "info":
		return diag.Info
	case "hint":
		return diag.Hint
	}

	return diag.Warning
}
