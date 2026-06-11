package fakeharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// Interactive engines speak a line protocol on stdin/stdout: requests are
// answered as they arrive, residents stay alive until stdin closes, and the
// sentinel trap is enforced per input line because stdin cannot be pre-read.

const sentinelTrapExit = 86

// engineLoop scans stdin lines, enforces the sentinel trap, and hands each
// non-empty line to handle. handle returns false to stop the engine.
func engineLoop(run personaRun, handle func(line []byte) bool) int {
	scanner := bufio.NewScanner(run.stdinRaw)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if run.sentinel != "" && strings.Contains(line, run.sentinel) {
			_, _ = fmt.Fprintln(run.stderr, "fake harness sentinel leak trap: sentinel observed in process input")
			return sentinelTrapExit
		}
		if !handle([]byte(line)) {
			return 0
		}
	}
	return 0
}

// codexAppServerEngine speaks the codex app-server JSONL RPC protocol:
// initialize/initialized handshake, thread/start, turn/start, then
// item/completed + tokenUsage + turn/completed notifications per turn.
func codexAppServerEngine(run personaRun) int {
	threadID := "fake-codex-thread"
	return engineLoop(run, func(line []byte) bool {
		var message struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &message); err != nil {
			run.out.line(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"parse error"}}`)
			return true
		}
		switch message.Method {
		case "initialize":
			run.out.linef(`{"id":%d,"result":{"userAgent":"fake-codex/0.0"}}`, idOrZero(message.ID))
		case "initialized":
			// notification, no response
		case "thread/start":
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if strings.TrimSpace(params.ThreadID) != "" {
				threadID = strings.TrimSpace(params.ThreadID)
			}
			run.out.linef(`{"id":%d,"result":{"thread":{"id":%q}}}`, idOrZero(message.ID), threadID)
		case "turn/start":
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if strings.TrimSpace(params.ThreadID) != "" {
				threadID = strings.TrimSpace(params.ThreadID)
			}
			turn := run.state.appendTurn(run.harness, threadID, "")
			run.out.linef(`{"id":%d,"result":{"turn":{"id":"fake-codex-turn-%d"}}}`, idOrZero(message.ID), turn)
			run.out.linef(`{"method":"item/completed","params":{"item":{"id":"item_%d","type":"agentMessage","text":"fake codex response%s"}}}`, turn, run.turnSuffix(turn))
			run.out.line(`{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"last":{"inputTokens":1,"outputTokens":1}}}}`)
			run.out.linef(`{"method":"turn/completed","params":{"turn_id":"fake-codex-turn-%d","status":"completed"}}`, turn)
		default:
			if message.ID != nil {
				run.out.linef(`{"id":%d,"result":{}}`, *message.ID)
			}
		}
		return true
	})
}

// piRPCEngine speaks pi's --mode rpc JSONL protocol: typed commands on stdin
// (prompt, steer, follow_up, get_state, new_session, switch_session,
// set_model), responses plus agent/message/turn events on stdout.
func piRPCEngine(run personaRun) int {
	sessionID := "fake-pi-session"
	if value, ok := flagValue(run.args, "--session"); ok && strings.TrimSpace(value) != "" {
		sessionID = piSessionIDFromRef(value)
	}
	return engineLoop(run, func(line []byte) bool {
		var command struct {
			Type        string `json:"type"`
			ID          string `json:"id"`
			Message     string `json:"message"`
			SessionPath string `json:"sessionPath"`
		}
		if err := json.Unmarshal(line, &command); err != nil {
			run.out.line(`{"type":"response","success":false,"error":"parse error"}`)
			return true
		}
		respond := func(commandName string, payload string) {
			id := ""
			if strings.TrimSpace(command.ID) != "" {
				id = fmt.Sprintf(",%q:%q", "id", command.ID)
			}
			if payload != "" {
				payload = "," + payload
			}
			run.out.linef(`{"type":"response","command":%q,"success":true%s%s}`, commandName, id, payload)
		}
		switch command.Type {
		case "prompt", "steer", "follow_up":
			turn := run.state.appendTurn(run.harness, sessionID, stdinSummary(command.Message))
			respond(command.Type, "")
			text := "fake pi response" + run.turnSuffix(turn)
			run.out.line(`{"type":"agent_start"}`)
			run.out.line(`{"type":"turn_start"}`)
			run.out.linef(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":%q}}`, text)
			run.out.linef(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}],"usage":{"input":1,"output":1},"stopReason":"stop"}}`, text)
			run.out.linef(`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]},"toolResults":[]}`, text)
			run.out.linef(`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":%q}]}]}`, text)
		case "get_state":
			respond("get_state", fmt.Sprintf(`"data":{"model":{"provider":"anthropic","modelId":"fake-pi-model"},"isStreaming":false,"sessionPath":%q,"sessionName":%q}`, run.state.sessionPath(run.harness, sessionID), sessionID))
		case "get_session_stats":
			respond("get_session_stats", fmt.Sprintf(`"data":{"turns":%d,"usage":{"input":1,"output":1}}`, run.state.turnCount(run.harness, sessionID)))
		case "new_session":
			sessionID = fmt.Sprintf("fake-pi-session-%d", run.state.turnCount(run.harness, sessionID)+1)
			respond("new_session", "")
		case "switch_session":
			if strings.TrimSpace(command.SessionPath) != "" {
				sessionID = piSessionIDFromRef(command.SessionPath)
			}
			respond("switch_session", "")
		case "set_model", "set_thinking_level", "abort":
			respond(command.Type, "")
		default:
			run.out.linef(`{"type":"response","command":%q,"success":false,"error":"unsupported command"}`, command.Type)
		}
		return true
	})
}

// grokACPEngine speaks a minimal ACP JSON-RPC dialect over stdio: initialize,
// session/new, session/load, then session/prompt answered with session/update
// notifications and a completion result.
func grokACPEngine(run personaRun) int {
	sessionID := "fake-grok-session"
	return engineLoop(run, func(line []byte) bool {
		var message struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &message); err != nil {
			run.out.line(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"parse error"}}`)
			return true
		}
		switch message.Method {
		case "initialize":
			run.out.linef(`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":1}}`, idOrZero(message.ID))
		case "session/new":
			run.out.linef(`{"jsonrpc":"2.0","id":%d,"result":{"sessionId":%q}}`, idOrZero(message.ID), sessionID)
		case "session/load":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if strings.TrimSpace(params.SessionID) != "" {
				sessionID = strings.TrimSpace(params.SessionID)
			}
			run.out.linef(`{"jsonrpc":"2.0","id":%d,"result":{}}`, idOrZero(message.ID))
		case "session/prompt":
			turn := run.state.appendTurn(run.harness, sessionID, "")
			text := "fake grok response" + run.turnSuffix(turn)
			run.out.linef(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":%q,"update":{"content":{"type":"text","text":%q}}}}`, sessionID, text)
			run.out.linef(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}}`, idOrZero(message.ID))
		default:
			if message.ID != nil {
				run.out.linef(`{"jsonrpc":"2.0","id":%d,"result":{}}`, *message.ID)
			}
		}
		return true
	})
}

func idOrZero(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}
