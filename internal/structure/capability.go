package structure

import (
	"regexp"
	"slices"
	"strings"
)

// Class is a kind of dangerous operation. What a hunk gains is what the after
// side calls into and the before side did not.
type Class string

// The classes, which are the operations worth knowing a change reached for.
const (
	ClassDelete  Class = "deletion"
	ClassExec    Class = "exec"
	ClassFile    Class = "filesystem"
	ClassNetwork Class = "network"
	ClassSQL     Class = "sql"
	ClassSecret  Class = "secrets"
)

// classes matches a call against what it reaches. It reads a name rather than
// resolving one, so a local named exec counts and a call reached through an
// alias does not: the honest claim is "a new capability visible to syntax",
// which still separates the changes that carry one from the ones that carry
// none.
//
// Every class but sql is matched against the callee alone, because a class
// found in an argument is usually a log line mentioning it. A statement is an
// argument, which is why sql is the exception.
var classes = map[Class]*regexp.Regexp{
	ClassDelete: regexp.MustCompile(`(?i)\b(rmtree|remove_all|removeall|unlink|rmdir|` +
		`truncate|drop_table|destroy_all|delete_many|deleteMany)\b`),
	ClassExec: regexp.MustCompile(`(?i)\b(exec|eval|system|popen|spawn|check_output|` +
		`check_call|subprocess|child_process|shell_exec)\b`),
	ClassFile: regexp.MustCompile(`\b(open|ReadFile|WriteFile|OpenFile|readFile|writeFile|` +
		`createWriteStream|Mkdir|MkdirAll|Chmod|Chown)\b`),
	ClassNetwork: regexp.MustCompile(`(?i)\b(requests|httpx|urlopen|urlretrieve|fetch|axios|` +
		`http\.get|http\.post|dial|socket|websocket)\b`),
	ClassSQL: regexp.MustCompile(`(?i)\b(execute|executemany|raw_query|from_statement|text)\b` +
		`.*\b(select|insert|update|delete|drop|alter)\b`),
	ClassSecret: regexp.MustCompile(`(?i)\b(getenv|environ|decrypt|encrypt|hmac|sign_|jwt|` +
		`token|secret|password|credential|private_key)\b`),
}

// gained is the classes the after side reaches and the before side did not, in
// a stable order so two runs over the same hunk rate it the same.
func gained(was, now []match) []Class {
	before, after := reached(was), reached(now)

	out := make([]Class, 0, len(after))

	for _, c := range after {
		if !slices.Contains(before, c) {
			out = append(out, c)
		}
	}

	slices.Sort(out)

	return out
}

// reached is every class the calls on one side match.
func reached(ms []match) []Class {
	var out []Class

	for _, m := range ms {
		if m.Rule != ruleCall {
			continue
		}

		name := callee(m.Text)

		for c, re := range classes {
			subject := name
			if c == ClassSQL {
				subject = m.Text
			}

			if !slices.Contains(out, c) && re.MatchString(subject) {
				out = append(out, c)
			}
		}
	}

	return out
}

// callee is what a call names, which is everything before its arguments.
func callee(text string) string {
	if i := strings.IndexByte(text, '('); i >= 0 {
		return text[:i]
	}

	return text
}
