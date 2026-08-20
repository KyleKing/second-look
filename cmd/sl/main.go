// Command sl prepares a code review locally and posts it in one call.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sl:", err)
		os.Exit(1)
	}
}
