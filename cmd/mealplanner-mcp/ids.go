package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errUnsupportedID = errors.New("unsupported request id: use a valid Unicode string or a canonical integer between -9007199254740991 and 9007199254740991")
var errQueueFull = errors.New("protocol input queue exceeds the 8 MiB limit")

var errFrameTooLarge = errors.New("protocol frame exceeds the 4 MiB limit")
var errAmbiguousEnvelope = errors.New("invalid protocol envelope: malformed JSON or duplicate/noncanonical member name")
var errAmbiguousToolParams = errors.New("invalid tools/call params: malformed JSON or duplicate/noncanonical member name")
var errDuplicateID = errors.New("request id already used in this session")
var errIDBudget = errors.New("session request ID budget exhausted (reconnect to start a new session)")

// The pinned SDK decodes IDs via float64 and then int64. Validate BEFORE handing
// any frame to it: accepting a rounded ID can correlate a response (or a
// cancellation) with the wrong request. MCP permits string or integer IDs; use
// strings for integers outside this interoperable range. We also reject numeric
// spellings the SDK cannot echo verbatim, including -0, 1.0 and 1e0.
// A rejected frame terminates the session without executing it or later frames.
// Earlier accepted requests still get the gate's normal shutdown grace period.
func supportedID(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return true
	}
	if t[0] == '"' {
		// Validate the original bytes before either decoder can replace malformed
		// Unicode and incorrectly correlate a response or cancellation.
		return draft.ValidJSONString(t)
	}
	n, err := strconv.ParseInt(string(t), 10, 64)
	return err == nil && n >= -9007199254740991 && n <= 9007199254740991 && strconv.FormatInt(n, 10) == string(t)
}

// strictMembers rejects duplicate decoded keys (including escaped aliases), and
// case variants of protocol members. SDK decoders do not all have the same
// duplicate-key/case matching semantics; never let them choose a different ID,
// method or cancellation target than the shutdown gate. Nested tool arguments
// retain their independent adapter validation and are not rewritten here.
func strictMembers(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, nil
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errAmbiguousEnvelope
		}
		if _, exists := out[key]; exists {
			return nil, errAmbiguousEnvelope
		}
		for _, name := range names {
			if key != name && strings.EqualFold(key, name) {
				return nil, errAmbiguousEnvelope
			}
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

// deliver validates a whole frame before registration or delivery to the SDK.
// It runs under the gate mutex, including at EOF and on a shutdown signal.
func (g *frameGate) deliver(line []byte) bool {
	// Without an LF at EOF or shutdown, a buffered CR is frame data.
	if len(line) > maxTrackedLine {
		g.rejectInput(errFrameTooLarge)
		return false
	}
	// Fail closed before request registration. The bounded queue deliberately
	// rejects overload rather than blocking while holding the gate mutex, which
	// would prevent responses, cancellation and shutdown from making progress.
	if g.queue.Len()+len(bytes.Trim(line, " \t\r\n"))+1 > maxQueuedBytes {
		g.rejectInput(errQueueFull)
		return false
	}
	// Each line is one complete JSON value. Reject malformed/concatenated
	// values before the SDK streaming decoder can consume a valid prefix.
	if !json.Valid(line) {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	f, err := strictMembers(line, "jsonrpc", "method", "id", "params", "result", "error")
	if err != nil || f == nil {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	// The SDK treats method:null as an empty request method. Require a valid
	// wire string before its decoder can coerce null or malformed Unicode.
	if method, exists := f["method"]; exists && !draft.ValidJSONString(method) {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	if id, exists := f["id"]; exists && !supportedID(id) {
		g.rejectInput(errUnsupportedID)
		return false
	}
	// Use the pinned SDK's parsed message to reserve the same ID it will use
	// for dispatch. Wrong versions or types must not reach the transport.
	msg, err := jsonrpc.DecodeMessage(line)
	if err != nil {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	// Check the original bytes before changing gate state or forwarding.
	// Admission still gives a malformed cancellation ID its specific error.
	validUTF8 := utf8.Valid(line)
	validEnvelope := draft.ValidUniqueJSONEnvelope(line, "jsonrpc", "method", "id", "params", "result", "error")
	if req, ok := msg.(*jsonrpc.Request); ok {
		forward, err := g.admitRequest(req, validUTF8, validEnvelope)
		if err != nil {
			g.rejectInput(err)
			return false
		}
		if !forward {
			return true
		}
	} else if !validEnvelope {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	// Preserve the JSON bytes while normalizing the frame-boundary whitespace
	// for the SDK, which requires LF/CR immediately after the value.
	g.queue.Write(bytes.Trim(line, " \t\r\n"))
	g.queue.WriteByte('\n')
	return true
}

// admitRequest reserves calls and returns whether the SDK should see the frame.
func (g *frameGate) admitRequest(req *jsonrpc.Request, validUTF8, validEnvelope bool) (bool, error) {
	if !validUTF8 && req.Method != "notifications/cancelled" {
		return false, errAmbiguousEnvelope
	}
	switch req.Method {
	case "tools/call":
		if len(req.Params) > 0 && !draft.ValidUniqueJSONMembers(req.Params, "name", "arguments", "_meta") {
			return false, errAmbiguousToolParams
		}
	case "notifications/cancelled":
		params, err := strictMembers(req.Params, "requestId", "reason", "_meta")
		target, hasTarget := params["requestId"]
		if err != nil || params == nil || req.IsCall() || !hasTarget {
			return false, errAmbiguousEnvelope
		}
		if !supportedID(target) {
			return false, errUnsupportedID
		}
		if !validEnvelope || !draft.ValidUniqueJSONMembers(req.Params) {
			return false, errAmbiguousEnvelope
		}
		// Invalid reason or _meta must not prematurely expose EOF.
		var cancellation mcp.CancelledParams
		if json.Unmarshal(req.Params, &cancellation) != nil {
			return false, errAmbiguousEnvelope
		}
		id, err := jsonrpc.MakeID(cancellation.RequestID)
		if err != nil || !id.IsValid() {
			return false, errUnsupportedID
		}
		if _, pending := g.pending[id]; !pending {
			// The SDK dispatches cancellation asynchronously. An unknown
			// target must not accidentally cancel a subsequent first use.
			return false, nil
		}
		g.release(id)
	default:
		if len(req.Params) > 0 && !draft.ValidUniqueJSONMembers(req.Params) {
			return false, errAmbiguousEnvelope
		}
	}
	if !validEnvelope {
		return false, errAmbiguousEnvelope
	}
	if req.IsCall() {
		if _, exists := g.seen[req.ID]; exists {
			// A response with this ID would be ambiguous. Reject the frame
			// without registering a response or forwarding it to the SDK.
			return false, errDuplicateID
		}
		text, _ := req.ID.Raw().(string)
		if len(g.seen) >= g.idBudget.count || len(text) > g.idBudget.bytes-g.seenBytes {
			return false, errIDBudget
		}
		g.seen[req.ID] = struct{}{}
		g.seenBytes += len(text)
		g.pending[req.ID] = struct{}{}
	}
	return true, nil
}

func (g *frameGate) rejectInput(err error) {
	g.inputErr = err
	g.stopped = true
	g.startGrace()
	g.lineBuf = nil
}
