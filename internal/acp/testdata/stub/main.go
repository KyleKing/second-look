// A stub ACP agent for the tests. It answers initialize, opens and loads
// sessions, and answers a prompt the way a real one does: message chunks and a
// tool call streaming in, a permission ask it blocks on, and the turn's end
// last. The prompt runs on its own goroutine so the read loop keeps routing —
// the permission reply arrives on the same stdin the requests do.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type frame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

var (
	mu  sync.Mutex
	out = bufio.NewWriter(os.Stdout)

	pmu     sync.Mutex
	pending = map[string]chan frame{}

	cmu      sync.Mutex
	cancelCh = make(chan struct{})
)

// cancelled is the channel a "hold" prompt waits on: session/cancel fires it
// and makes a fresh one for the next holder.
func cancelled() chan struct{} {
	cmu.Lock()
	defer cmu.Unlock()

	return cancelCh
}

func fireCancel() {
	cmu.Lock()
	defer cmu.Unlock()

	close(cancelCh)

	cancelCh = make(chan struct{})
}

func send(v any) {
	mu.Lock()
	defer mu.Unlock()

	//nolint:errcheck // a stub's write failing ends the test that reads it
	b, _ := json.Marshal(v)
	//nolint:errcheck // same
	_, _ = out.Write(b)
	//nolint:errcheck // same
	_ = out.WriteByte('\n')
	//nolint:errcheck // same
	_ = out.Flush()
}

func result(id json.RawMessage, v any) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "result": v})
}

func notify(session, update string, params map[string]any) {
	params["sessionUpdate"] = update
	send(map[string]any{
		"jsonrpc": "2.0", "method": "session/update",
		"params": map[string]any{"sessionId": session, "update": params},
	})
}

func say(session, text string) {
	notify(session, "agent_message_chunk", map[string]any{
		"messageId": "m1", "content": map[string]any{"type": "text", "text": text},
	})
}

// ask sends a request of the client and waits out the response, which the read
// loop routes back by id — the raw frame bytes, quotes and all.
func ask(id string, params map[string]any) frame {
	raw, _ := json.Marshal(id) //nolint:errcheck // a string always marshals
	key := string(raw)

	ch := make(chan frame, 1)

	pmu.Lock()
	pending[key] = ch
	pmu.Unlock()

	send(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "session/request_permission", "params": params,
	})

	got := <-ch

	pmu.Lock()
	delete(pending, key)
	pmu.Unlock()

	return got
}

// modes is what the stub advertises: one ordinary mode and one the tests can
// set to watch session/set_mode land.
var modes = map[string]any{
	"availableModes": []map[string]any{
		{"id": "code", "name": "Code"},
		{"id": "bypass", "name": "Bypass Permissions"},
	},
	"currentModeId": "code",
}

func prompt(f frame) {
	var p struct {
		SessionID string `json:"sessionId"`
		Prompt    []struct {
			Text string `json:"text"`
		} `json:"prompt"`
	}
	//nolint:errcheck // a malformed prompt is the test's own bug
	_ = json.Unmarshal(f.Params, &p)

	say(p.SessionID, "stub heard: "+p.Prompt[0].Text)

	text := p.Prompt[0].Text

	switch {
	case strings.HasPrefix(text, "tools:"):
		tools(f, p.SessionID, text)

		return
	case strings.HasPrefix(text, "fast:"):
		result(f.ID, map[string]any{"stopReason": "end_turn"})

		return
	case strings.HasPrefix(text, "wait:"):
		rest := strings.TrimPrefix(text, "wait:")
		ms, _, _ := strings.Cut(rest, " ")

		if n, err := strconv.Atoi(ms); err == nil {
			time.Sleep(time.Duration(n) * time.Millisecond)
		}

		result(f.ID, map[string]any{"stopReason": "end_turn"})

		return
	case text == "hold":
		<-cancelled()
		result(f.ID, map[string]any{"stopReason": "cancelled"})

		return
	}

	notify(p.SessionID, "tool_call", map[string]any{
		"toolCallId": "t1", "title": "runs the tests", "status": "in_progress",
	})

	got := ask("perm-1", map[string]any{
		"sessionId": p.SessionID,
		"toolCall":  map[string]any{"toolCallId": "t1", "title": "runs the tests"},
		"options": []map[string]any{
			{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
			{"optionId": "deny", "name": "Reject", "kind": "reject_once"},
		},
	})

	var outcome struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	//nolint:errcheck // same
	_ = json.Unmarshal(got.Result, &outcome)

	said := outcome.Outcome.Outcome
	if said == "selected" {
		said = "selected " + outcome.Outcome.OptionID
	}

	notify(p.SessionID, "tool_call_update", map[string]any{
		"toolCallId": "t1", "status": "completed",
		"content": []map[string]any{
			{"type": "content", "content": map[string]any{"type": "text", "text": "the suite passed"}},
		},
	})
	say(p.SessionID, "permission said: "+said)
	result(f.ID, map[string]any{"stopReason": "end_turn"})
}

// tools answers a "tools:N" prompt with N calls streaming by, each finishing
// with a text output block, so the pane's collapse has a run to fold.
func tools(f frame, session, text string) {
	n, err := strconv.Atoi(strings.TrimPrefix(text, "tools:"))
	if err != nil || n < 1 {
		n = 1
	}

	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("tool-%d", i)
		notify(session, "tool_call", map[string]any{
			"toolCallId": id, "title": fmt.Sprintf("tool number %d", i), "status": "in_progress",
		})
		notify(session, "tool_call_update", map[string]any{
			"toolCallId": id, "status": "completed",
			"content": []map[string]any{
				{"type": "content", "content": map[string]any{
					"type": "text", "text": fmt.Sprintf("output of tool %d", i),
				}},
			},
		})
	}

	say(session, fmt.Sprintf("%d tools done", n))
	result(f.ID, map[string]any{"stopReason": "end_turn"})
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)

	for sc.Scan() {
		var f frame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			fmt.Fprintf(os.Stderr, "stub: %v\n", err)

			continue
		}

		if f.Method == "" {
			pmu.Lock()
			ch := pending[string(f.ID)]
			pmu.Unlock()

			if ch != nil {
				ch <- f
			}

			continue
		}

		switch f.Method {
		case "initialize":
			result(f.ID, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
				"agentInfo":         map[string]any{"name": "stub", "version": "0.1"},
				"authMethods":       []any{},
			})
		case "session/new":
			result(f.ID, map[string]any{"sessionId": "stub-session", "modes": modes})
		case "session/load":
			var p struct {
				SessionID string `json:"sessionId"`
			}
			//nolint:errcheck // same
			_ = json.Unmarshal(f.Params, &p)
			say(p.SessionID, "reloaded transcript")
			result(f.ID, map[string]any{"modes": modes})
		case "session/set_mode":
			var p struct {
				SessionID string `json:"sessionId"`
				ModeID    string `json:"modeId"`
			}
			//nolint:errcheck // same
			_ = json.Unmarshal(f.Params, &p)
			modes["currentModeId"] = p.ModeID
			say(p.SessionID, "mode set: "+p.ModeID)
			result(f.ID, map[string]any{})
		case "session/prompt":
			go prompt(f)
		case "session/cancel":
			fireCancel()
			result(f.ID, map[string]any{})
		case "authenticate":
			result(f.ID, map[string]any{})
		default:
			result(f.ID, map[string]any{})
		}
	}
}
