// M3-T2/ADR-0012 follow-up tests for the
// `parseVerdictJSON[T]` generic helper that
// consolidates the two near-clone brace scanners
// in `evaluate.go` and `compare.go`.
//
// The corpus is the five cases in ADR-0012 §D3
// plus the M3-T1-era per-phase round-trip tests
// (already in `compare_test.go` and the evaluate
// test file). These tests pin the helper's
// behavior independently of either call site.
package engine

import (
	"errors"
	"testing"
)

// minimalVerdict is a one-field struct used to
// exercise the generic helper without coupling to
// the evalVerdict or comparisonVerdict types.
type minimalVerdict struct {
	Winner string `json:"winner"`
}

func TestParseVerdictJSON_HappyPath(t *testing.T) {
	v, err := parseVerdictJSON[minimalVerdict](`{"winner":"new"}`)
	if err != nil {
		t.Fatalf("parseVerdictJSON: %v", err)
	}
	if v.Winner != "new" {
		t.Errorf("Winner = %q, want new", v.Winner)
	}
}

// TestParseVerdictJSON_LenientWrapping covers
// ADR-0012 §D3 row 1: the JSON may be inside a
// code fence or preceded by prose.
func TestParseVerdictJSON_LenientWrapping(t *testing.T) {
	cases := []string{
		`Here's the verdict: {"winner":"previous"}`,
		"```json\n{\"winner\":\"none\"}\n```",
		"Some prose before\n{\"winner\":\"new\"}\nsome prose after",
	}
	for _, c := range cases {
		v, err := parseVerdictJSON[minimalVerdict](c)
		if err != nil {
			t.Errorf("parseVerdictJSON(%q): %v", c, err)
			continue
		}
		if v.Winner == "" {
			t.Errorf("parseVerdictJSON(%q): Winner is empty", c)
		}
	}
}

// TestParseVerdictJSON_NoJSON covers ADR-0012
// §D3 row 2: a response with no JSON object at
// all is a hard error.
func TestParseVerdictJSON_NoJSON(t *testing.T) {
	_, err := parseVerdictJSON[minimalVerdict]("no JSON here, just prose")
	if err == nil {
		t.Fatal("parseVerdictJSON: no error for no-JSON input")
	}
	var vpe *VerdictParseError
	if !errors.As(err, &vpe) {
		t.Errorf("error = %v, want errors.As *VerdictParseError", err)
	}
}

// TestParseVerdictJSON_Unterminated covers the
// "open brace but no close" failure mode.
func TestParseVerdictJSON_Unterminated(t *testing.T) {
	_, err := parseVerdictJSON[minimalVerdict](`{"winner":"new"`)
	if err == nil {
		t.Fatal("parseVerdictJSON: no error for unterminated JSON")
	}
	var vpe *VerdictParseError
	if !errors.As(err, &vpe) {
		t.Errorf("error = %v, want errors.As *VerdictParseError", err)
	}
}

// TestParseVerdictJSON_EmbeddedBraceInString
// covers the §D3 row 4 corpus: an embedded '}'
// inside a string must not fool the depth counter.
func TestParseVerdictJSON_EmbeddedBraceInString(t *testing.T) {
	v, err := parseVerdictJSON[minimalVerdict](`{"winner":"contains}brace"}`)
	if err != nil {
		t.Fatalf("parseVerdictJSON: %v", err)
	}
	if v.Winner != "contains}brace" {
		t.Errorf("Winner = %q, want %q", v.Winner, "contains}brace")
	}
}

// TestParseVerdictJSON_Generics: the helper
// works for both evalVerdict and comparisonVerdict
// (the two real call sites). This test pins the
// generic contract; if a future contributor
// changes the helper to be type-specific, this
// test fails.
func TestParseVerdictJSON_Generics(t *testing.T) {
	// evalVerdict: includes a numeric field.
	ev, err := parseVerdictJSON[evalVerdict](`{"passed":true,"score":0.85,"summary":"ok"}`)
	if err != nil {
		t.Fatalf("parseVerdictJSON[evalVerdict]: %v", err)
	}
	if !ev.Passed || ev.Score != 0.85 || ev.Summary != "ok" {
		t.Errorf("evalVerdict = %+v, want passed=true score=0.85 summary=ok", ev)
	}
	// comparisonVerdict: includes a string field.
	cv, err := parseVerdictJSON[comparisonVerdict](`{"winner":"new","confidence":0.9,"reasons":["ok"],"missing_requirements":[]}`)
	if err != nil {
		t.Fatalf("parseVerdictJSON[comparisonVerdict]: %v", err)
	}
	if cv.Winner != "new" || cv.Confidence != 0.9 {
		t.Errorf("comparisonVerdict = %+v, want winner=new confidence=0.9", cv)
	}
}
