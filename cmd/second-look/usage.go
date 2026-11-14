package main

import (
	"fmt"
	"io"
	"time"

	"github.com/kyleking/aragonite/keyusage"

	"github.com/kyleking/second-look/internal/tui"
)

const usageApp = "second-look"

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

	u, err := keyusage.Load(dir, km)
	if err != nil {
		return fmt.Errorf("reading the usage log: %w", err)
	}

	if err := keyusage.Write(stdout, u, km, time.Now().Add(-tui.StaleAfter)); err != nil {
		return fmt.Errorf("writing the key report: %w", err)
	}

	return nil
}
