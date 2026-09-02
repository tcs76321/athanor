// ADR-0012 follow-up: a single generic helper
// `parseVerdictJSON[T]` consolidates the two
// near-clone brace scanners in `evaluate.go` and
// `compare.go` (M3-T1 added them separately, when
// the two parsers' structure was still evolving).
//
// Both parsers share the same shape:
//
//  1. Find the first '{' in the LLM's content
//     (lenient about wrapping prose / code fences).
//  2. Track depth (with quote + escape handling)
//     to find the matching '}'.
//  3. Unmarshal the substring into the verdict
//     type.
//
// The only thing that varies is the destination
// type. Go's generics (1.18+) make this a
// one-function refactor; the test corpus is the
// five cases in ADR-0012 §D3 plus the per-phase
// tests that already cover the two parsers'
// round-trips.
package engine

import (
	"encoding/json"
	"fmt"
)

// parseVerdictJSON extracts the first JSON object
// in `content` and unmarshals it into a value of
// type T. The helper is lenient about wrapping:
// the JSON may be inside a code fence or preceded
// by prose. The matching '}' is found by tracking
// brace depth with quote + escape handling (an
// embedded '}' inside a string does not count).
//
// Errors are returned as a wrapped *VerdictParseError
// so the caller can use errors.As to branch on the
// specific failure mode (no JSON, unterminated,
// unmarshal failure).
func parseVerdictJSON[T any](content string) (T, error) {
	var v T
	start := -1
	for i, c := range content {
		if c == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return v, &VerdictParseError{Msg: fmt.Sprintf("no JSON object in verdict: %q", content)}
	}
	depth, end := 0, -1
	inStr, escape := false, false
	for i := start; i < len(content); i++ {
		c := content[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return v, &VerdictParseError{Msg: fmt.Sprintf("unterminated JSON object in verdict: %q", content)}
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
		return v, &VerdictParseError{Msg: fmt.Sprintf("decoding verdict JSON: %v", err)}
	}
	return v, nil
}

// VerdictParseError is the typed error
// parseVerdictJSON returns. Callers can use
// errors.As to branch on the specific failure
// mode (e.g. compare.go's "unknown winner" path
// uses the same package-level error type, but
// can be matched separately from this one).
type VerdictParseError struct {
	Msg string
}

func (e *VerdictParseError) Error() string { return e.Msg }
