// Command stub is a language server that answers enough of the protocol to
// drive the client without installing one.
//
// It is deliberately awkward in the three ways a real server is: it will not
// publish anything until its own request has been answered, it publishes for
// files nobody asked about, and it corrects itself after a delay. Each of those
// has broken this client once.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type frame struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
}

func main() {
	br := bufio.NewReader(os.Stdin)
	answered := make(chan struct{})

	var once bool

	for {
		body, err := read(br)
		if err != nil {
			return
		}

		var f frame
		if err := json.Unmarshal(body, &f); err != nil {
			continue
		}

		switch {
		case f.Method == "initialize":
			send(map[string]any{"id": f.ID, "result": map[string]any{"capabilities": map[string]any{}}})
		case f.Method == "initialized":
			// Nothing is published until this is answered, which is what a
			// client that ignores a server's requests hangs on.
			send(map[string]any{"id": json.RawMessage("9001"), "method": "workspace/configuration"})
		case f.Method == "textDocument/hover":
			send(map[string]any{"id": f.ID, "result": hover(f.Params)})
		case f.Method == "textDocument/didOpen", f.Method == "textDocument/didChange":
			uri := uriOf(f.Params)

			go func() {
				<-answered
				publish(uri)
			}()
		case f.ID != nil && f.Method == "" && !once:
			once = true

			close(answered)
		}
	}
}

// publish answers for a file nobody asked about, then for the one that was
// opened, and then corrects itself once the project is loaded.
func publish(uri string) {
	diagnostics(uri+".other", []map[string]any{{
		"range":    rng(0),
		"severity": 1,
		"message":  "a file nobody asked about",
	}})

	diagnostics(uri, nil)

	time.Sleep(500 * time.Millisecond)

	// Noise for a file nobody asked about never stops, which is what a monorepo
	// does and what must not hold the pass open.
	go func() {
		for {
			diagnostics(uri+".other", []map[string]any{{"range": rng(0), "message": "still noisy"}})
			time.Sleep(200 * time.Millisecond)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	diagnostics(uri, []map[string]any{{
		"range":    rng(2),
		"severity": 1,
		"code":     "2339",
		"source":   "stub",
		"message":  "Property 'is_archived' does not exist on type 'Row'.",
	}})
}

func diagnostics(uri string, ds []map[string]any) {
	if ds == nil {
		ds = []map[string]any{}
	}

	send(map[string]any{
		"method": "textDocument/publishDiagnostics",
		"params": map[string]any{"uri": uri, "diagnostics": ds},
	})
}

func rng(line int) map[string]any {
	return map[string]any{
		"start": map[string]any{"line": line, "character": 0},
		"end":   map[string]any{"line": line, "character": 4},
	}
}

// hover answers about the name at the character asked for, so a client asking
// once per name gets one answer per name.
func hover(params json.RawMessage) any {
	var p struct {
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}

	if p.Position.Character > 20 {
		return nil
	}

	return map[string]any{
		"contents": map[string]any{
			"kind": "markdown",
			"value": fmt.Sprintf("```typescript\nconst at%d: number\n```\nprose nobody needs",
				p.Position.Character),
		},
	}
}

func uriOf(params json.RawMessage) string {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}

	_ = json.Unmarshal(params, &p)

	return p.TextDocument.URI
}

func send(msg map[string]any) {
	msg["jsonrpc"] = "2.0"

	body, err := json.Marshal(msg)
	if err != nil {
		return
	}

	fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func read(br *bufio.Reader) ([]byte, error) {
	n := -1

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		if name, value, ok := strings.Cut(line, ":"); ok &&
			strings.EqualFold(strings.TrimSpace(name), "content-length") {
			if n, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, err
			}
		}
	}

	if n < 0 {
		return nil, io.EOF
	}

	body := make([]byte, n)
	_, err := io.ReadFull(br, body)

	return body, err
}
