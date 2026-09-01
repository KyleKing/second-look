package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/kyleking/second-look/internal/get"
)

// confirm is the question a person answers, or a standing no when nobody is
// there to answer it: an agent's run never has its working tree moved.
func confirm(stdin io.Reader, stdout io.Writer) get.Confirm {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return declined
	}

	return asking(stdin, stdout)
}

func declined(string) (bool, error) { return false, nil }

// asking reads a yes-or-no answer off stdin. Anything but y or yes is no,
// because the question is only ever asked before something that moves the
// working tree.
func asking(stdin io.Reader, stdout io.Writer) get.Confirm {
	// One reader for every question, because a fresh one per call would discard
	// whatever the last read buffered past its newline.
	in := bufio.NewReader(stdin)

	return func(question string) (bool, error) {
		if err := write(stdout, question+" [y/N] "); err != nil {
			return false, err
		}

		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return false, fmt.Errorf("reading your answer: %w", err)
		}

		answer := strings.ToLower(strings.TrimSpace(line))

		return answer == "y" || answer == "yes", nil
	}
}
