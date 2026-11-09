package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

var errUnsupportedID = errors.New("unsupported request id: use a string or a canonical integer between -9007199254740991 and 9007199254740991")
var errFrameTooLarge = errors.New("protocol frame exceeds the 4 MiB limit")

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
		return true
	} // JSON syntax has already been checked.
	n, err := strconv.ParseInt(string(t), 10, 64)
	return err == nil && n >= -9007199254740991 && n <= 9007199254740991 && strconv.FormatInt(n, 10) == string(t)
}

// deliver validates a whole frame before registration or delivery to the SDK.
// It runs under the gate mutex, including at EOF and on a shutdown signal.
var errAmbiguousEnvelope = errors.New("invalid protocol envelope: malformed JSON or duplicate/noncanonical member name")

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

func (g *frameGate) deliver(line []byte) bool {
	// Each line is one complete JSON value. Reject malformed/concatenated
	// values before the SDK streaming decoder can consume a valid prefix.
	if !json.Valid(line) {
		g.rejectInput(errAmbiguousEnvelope)
		return false
	}
	{
		f, err := strictMembers(line, "jsonrpc", "method", "id", "params", "result", "error")
		if err != nil || f == nil {
			g.rejectInput(errAmbiguousEnvelope)
			return false
		}
		if f != nil {
			if id, exists := f["id"]; exists && !supportedID(id) {
				g.rejectInput(errUnsupportedID)
				return false
			}
			var method string
			if json.Unmarshal(f["method"], &method) == nil && method == "notifications/cancelled" {
				params, err := strictMembers(f["params"], "requestId", "reason", "_meta")
				if err != nil {
					g.rejectInput(errAmbiguousEnvelope)
					return false
				}
				if id, exists := params["requestId"]; exists && !supportedID(id) {
					g.rejectInput(errUnsupportedID)
					return false
				}
			}
		}
	}
	g.line(line)
	g.queue.Write(line)
	return true
}

func (g *frameGate) rejectInput(err error) {
	g.inputErr = err
	g.stopped = true
	g.lineBuf = nil
}
