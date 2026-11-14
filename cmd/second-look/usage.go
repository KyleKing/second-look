package main

import (
	"fmt"
	"io"
	"time"

	"github.com/kyleking/aragonite/keyusage"

	"github.com/kyleking/second-look/internal/tui"
)

const usageApp = "second-look"

const (
	cmdAgent    = "agent"
	cmdComment  = "comment"
	cmdThreads  = "threads"
	dryRunFlag  = "--dry-run"
	helpFlag    = "--help"
	versionFlag = "--version"
)

var cliRoot = keyusage.Command{
	Name:  usageApp,
	What:  "review the current branch's pull request",
	Flags: []string{"-h", helpFlag, "-v", versionFlag},
	Subs: []keyusage.Command{
		{Name: cmdAgent, What: "check or run the configured agent", Subs: []keyusage.Command{
			{Name: "check", What: "check the agent starts"},
		}},
		{Name: cmdComment, What: "stage comments", Subs: []keyusage.Command{
			{Name: "add", What: "stage comments from a JSON batch"},
		}},
		{Name: "context", What: "one comment with its hunk, note, and thread"},
		{Name: "gc", What: "drop finished reviews and sweep caches", Flags: []string{dryRunFlag, "--stale"}},
		{Name: "get", What: "fetch the pull request and prepare the review"},
		{Name: "image", What: "fetch an image"},
		{Name: "inbox", What: "the review queue", Flags: []string{jsonArg}},
		{Name: "journal", What: "read the usage journal back"},
		{Name: "keys", What: "which keys you use and never use"},
		{Name: "local", What: "review this checkout's diff", Flags: []string{"--base", "--branch", "--range", "-r"}},
		{Name: "plan", What: "stage the reading plan", Subs: []keyusage.Command{
			{Name: "ask", What: "stage a question"},
			{Name: "chapter", What: "stage a chapter"},
			{Name: "claim", What: "stage a claim"},
			{Name: "mode", What: "set the reading mode"},
			{Name: "stop", What: "stage a stop"},
		}},
		{Name: "post", What: "post the review", Flags: []string{dryRunFlag, "--fixes", "--only"}},
		{Name: "reviews", What: "what is staged locally", Flags: []string{jsonArg}},
		{Name: "session", What: "record the agent session"},
		{Name: "show", What: "print the prepared review", Flags: []string{"--diff", "--payload", "--threads"}},
		{Name: "skill", What: "print the agent instructions"},
		{Name: "status", What: "one line of what the queue holds"},
		{Name: cmdThreads, What: "conversations that moved", Flags: []string{jsonArg}},
		{Name: "todo", What: "every comment an agent still owes work on"},
	},
}

// openUsage is the recorder for this session and what earlier sessions logged.
// Both are nil when recording is off, and seen is nil when the log cannot be
// read, so a legend brightens nothing rather than everything.
func openUsage() (*keyusage.Recorder, *keyusage.Usage) {
	rec := keyusage.Open(usageApp)
	if rec == nil {
		return nil, nil
	}

	u, err := keyusage.Load(keyusage.Dir(usageApp), tui.Keymap(queueTabs()))
	if err != nil {
		return rec, nil
	}

	return rec, &u
}

// keysCmd reads the usage log back against the keymap.
func keysCmd(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errUsageKeys
	}

	dir := keyusage.Dir(usageApp)
	if dir == "" {
		return write(stdout, "no usage log: set "+keyusage.EnvVar(usageApp)+" or create ~/Sync\n")
	}

	km := tui.Keymap(queueTabs())
	km[keyusage.CLI] = keyusage.Commands(cliRoot)

	u, err := keyusage.Load(dir, km)
	if err != nil {
		return fmt.Errorf("reading the usage log: %w", err)
	}

	if err := keyusage.Write(stdout, u, km, time.Now().Add(-tui.StaleAfter)); err != nil {
		return fmt.Errorf("writing the key report: %w", err)
	}

	return nil
}
