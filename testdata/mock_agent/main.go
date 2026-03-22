package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}

func send(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintln(os.Stdout, string(data))
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var m msg
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}

		switch m.Method {
		case "initialize":
			send(msg{
				JSONRPC: "2.0",
				ID:      m.ID,
				Result: map[string]any{
					"protocolVersion": 1,
					"agentName":       "mock-agent",
				},
			})

		case "session/new":
			send(msg{
				JSONRPC: "2.0",
				ID:      m.ID,
				Result:  map[string]any{"sessionId": "mock-session-001"},
			})

		case "session/prompt":
			var params struct {
				Prompt string `json:"prompt"`
			}
			_ = json.Unmarshal(m.Params, &params)
			payload, _ := json.Marshal(map[string]any{
				"type":    "text",
				"content": "Echo: " + params.Prompt,
				"done":    true,
			})
			send(msg{
				JSONRPC: "2.0",
				Method:  "session/update",
				Params:  json.RawMessage(payload),
			})
			send(msg{
				JSONRPC: "2.0",
				ID:      m.ID,
				Result:  map[string]any{},
			})

		case "session/cancel":
			send(msg{
				JSONRPC: "2.0",
				ID:      m.ID,
				Result:  map[string]any{"status": "cancelled"},
			})
			return
		}
	}
}
