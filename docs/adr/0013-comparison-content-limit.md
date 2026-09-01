# ADR 0013 — Comparison content limit is config-driven; prompt discloses the full size

**Status:** Accepted · **Date:** 2026-09-01 · **Refs:** ARCHITECTURE §19.3, §20.3; ROADMAP M3-T2 carry-over (GLM review of 2026-09-01)

## Context

The §13.1 Phase 6 (Comparing) prompt is built by `buildComparisonInstructions` (`internal/engine/compare.go:173–187`). It embeds the candidate artifact's content so the `security` persona can reason about it, then asks for the §19.3 verdict JSON. The prompt ends with:

```
New artifact content (truncated to 4 KB):
<content>
```

The `4 KB` is the literal value `4096` in the source (`compare.go:174`):

```go
content, _ := osReadFileLimited(final.StoragePath, 4096)
```

`osReadFileLimited` (`compare.go:192–207`) opens the file, reads up to `limit` bytes, returns the string. Failures are non-fatal — the comparison proceeds with an empty content section.

The issue is threefold:

1. **The limit is a magic number.** 4096 is the right size for the default `security` persona's 8192 context target (with EvaluationRecord rows consuming the rest), but the trade-off between "more content for the judge to reason about" and "fewer context tokens for the rest of the prompt" is operator-tunable. A 32B `security` persona with a 32K context could comfortably use 16 KB; a 3B `security` persona on a tight 4K context needs 2 KB. Today the choice is fixed in the source.

2. **The prompt discloses the truncation generically but not the actual size.** The label "truncated to 4 KB" is present (`compare.go:183`), but a 23 KB artifact shows the same label as a 4 KB artifact. The judge cannot self-calibrate its confidence on the evidence it actually saw.

3. **The verdict is fundamentally partial when the artifact is bigger than the limit.** A code artifact with the closing brace of a function on line 600 may be the *whole* defect that the judge would have caught. Today the judge reports its confidence on a prefix and the engine treats that confidence as the §19.3 signal.

The §19.3 deterministic guard (`compare.go:101–124`) requires `confidence > min_judge_confidence` (default 0.7) for an LLM `winner: "new"` verdict to stand. A judge that doesn't know how much of the artifact it saw is producing an under-calibrated confidence score. That score is the input to the guard; the guard cannot compensate for an uncalibrated input.

The fix is small and orthogonal to the §19.3 rule itself: make the limit config-driven, return the full file size, and print it in the prompt so the judge can self-calibrate.

## Decision

Two coordinated changes:

1. **Add `Execution.ComparisonContentLimit` to the config (default 4096 bytes, validate 256–65536).**
2. **Return the full file size from the read helper and include it in the comparison prompt.**

### D1. Configuration

`internal/config/config.go:164–174` — the `Execution` struct gains one field:

```go
type Execution struct {
    // ... existing fields ...
    // ComparisonContentLimit is the maximum number of bytes of the
    // candidate artifact's content embedded in the §13.1 Phase 6
    // (Comparing) prompt (ADR-0013). The default 4096 is sized for
    // the 8192-context security persona; raise it for a larger
    // persona with a larger context target. The §19.3 confidence
    // calibration is only as good as the artifact fragment the
    // judge actually saw.
    ComparisonContentLimit int `yaml:"comparison_content_limit"`
}
```

`internal/config/defaults.go:84–104` — the existing `applyDefaults` block for `Execution` gains:

```go
if c.Execution.ComparisonContentLimit == 0 {
    c.Execution.ComparisonContentLimit = 4096
}
```

`internal/config/validate.go:74–86` — the existing `validateRaw` block gains:

```go
if n := c.Execution.ComparisonContentLimit; n != 0 && (n < 256 || n > 65536) {
    return fmt.Errorf("execution.comparison_content_limit must be in [256, 65536], got %d", n)
}
```

A zero value is the default-applied value (so absent YAML → 4096). A non-zero value outside the range is rejected at boot. The lower bound (256) is the minimum that produces a useful prompt fragment; the upper bound (65 536) is the largest that still leaves the §19.3 confidence-calibration story defensible (a 64 KB fragment inside a 64 K prompt is "the whole artifact" by another name and probably wants a different mechanism than a truncation).

### D2. The read helper extension

The current helper returns `(string, error)`. After this ADR it returns the full file size alongside the truncated content:

```go
// readArtifactContentForComparison reads up to limit bytes from
// path. The first return is the full file size in bytes (0 if
// the file does not exist or could not be stat'd). The second is
// the truncated content. The full size is used by the comparison
// prompt to tell the judge how much of the artifact it actually
// saw.
func readArtifactContentForComparison(path string, limit int) (fullSize int64, content string, err error)
```

The implementation calls `os.Stat` first (cheap on the local FS), then opens and reads up to `limit` bytes. A non-existent file returns `fullSize=0, content="", err=nil` (the existing non-fatal behavior is preserved — the comparison proceeds with the empty content section and the prompt discloses `full_size: 0 bytes`).

The helper moves from `compare.go` to a new `internal/engine/artifact_io.go` (small file, single responsibility). The existing `osReadFileLimited` is renamed and its signature changes; the only call site is `buildComparisonInstructions`.

### D3. The prompt change

`buildComparisonInstructions` (`compare.go:173–187`) becomes:

```go
fullSize, content, _ := readArtifactContentForComparison(final.StoragePath, e.cfg.Execution.ComparisonContentLimit)
fmt.Fprintf(&b, "\nNew artifact content (showing first %d of %d bytes; if the artifact is truncated, your confidence should reflect the partial evidence):\n", len(content), fullSize)
b.WriteString(content)
```

The prompt now states:

- the byte count shown (the truncated length, which equals `limit` when the artifact is bigger),
- the full size of the artifact (`fullSize`),
- the explicit instruction that partial evidence should produce a calibrated confidence.

When `fullSize <= limit`, the prompt says "showing all N bytes" — the truncation is a no-op and the judge knows it. When `fullSize > limit`, the prompt says "showing 4096 of 23 481 bytes" and the judge has the signal to lower its confidence.

### D4. The `comparison` audit event gains two new fields

The audit event (`compare.go:126–135`) gains two fields:

```go
"content_truncated_bytes":  limit,             // bytes shown
"content_full_bytes":      fullSize,          // bytes total (== limit when no truncation)
```

A future quality probe (M3-T7) can correlate `content_truncated_bytes < content_full_bytes` with `verdict.confidence` to measure whether the judge self-calibrates.

### D5. Tests

Three test layers:

1. **`TestReadArtifactContentForComparison`** (new `internal/engine/artifact_io_test.go`):
   - empty path → `0, "", nil`
   - file smaller than limit → `fullSize=N, content=N bytes, nil`
   - file larger than limit → `fullSize=23481, content=4096 bytes, nil`
   - nonexistent file → `0, "", nil` (non-fatal)
2. **`TestBuildComparisonInstructions_DisclosesFullSize`** — write a 10 KB temp file, call `buildComparisonInstructions` with `ComparisonContentLimit: 4096`, assert the prompt contains `showing first 4096 of 10240 bytes`.
3. **`TestBuildComparisonInstructions_NoTruncationLabel`** — write a 1 KB temp file, call with `ComparisonContentLimit: 4096`, assert the prompt contains `showing all 1024 bytes` (or equivalent no-truncation phrasing).
4. **`TestConfig_ComparisonContentLimitValidation`** — extend `internal/config/config_test.go` with the new field's positive/negative cases (default, lower bound 256, upper bound 65536, out-of-range rejection).

### D6. The `config.example.yaml` example file

`config.example.yaml:75–87` — the `execution:` block gains:

```yaml
# Comparison prompt: max bytes of the candidate artifact's content
# embedded in the §13.1 Phase 6 prompt. 4096 fits the 8192-context
# security persona; raise for a larger persona. The prompt
# always discloses the full artifact size so the judge can
# calibrate its confidence.
comparison_content_limit: 4096
```

The `config.example.yaml` is test-enforced to match `defaults.go`; the new field's default value is in both files, so the test continues to pass.

## Consequences

- The §19.3 confidence input is now calibrated to the evidence the judge actually saw. A judge that sees 4096 of 23 481 bytes can report a confidence that reflects the partial view. The §19.3 deterministic guard is unchanged; the calibration of its input is improved.
- The 4 KB default is preserved. Operators with a larger `security` persona (27B/32B) can raise it; operators with a smaller persona can lower it. The boot-time validation rejects nonsensical values.
- The `osReadFileLimited` helper is renamed and its signature changes. The new helper moves to a small dedicated file (`internal/engine/artifact_io.go`) so the engine package's file I/O surface is auditable in one place.
- A future `M3-T7` quality probe can use the new `comparison` audit fields to measure calibration empirically. The probe is the right place to decide whether the default should change; this ADR only fixes the *shape* of the data.
- The existing `TestRun_ComparisonPicksPreviousWhenNewIsWorse` (E4 in `multicandidate_test.go`) and the rest of the dialectical-loop test suite are unaffected at the wire level. The prompt text changes (the "truncated to 4 KB" line becomes "showing first N of M bytes"), so any test that asserts on the *exact* prompt text needs an update; no such test exists in the current suite (assertions are on verdict semantics, not prompt text).

## Not in M3

- A per-archetype content limit. A `code` artifact's first 4 KB may be the function signature; a `text` artifact's first 4 KB is the introduction. Per-archetype tuning is a M4 concern once archetypes have measurable prompt-effect data from M3-T7.
- An automatic "judge asked to look at the omitted tail" mechanism (e.g., a second comparison call with the tail content). The current architecture is one comparison call per job; M3-T3 may revisit that.
- A streaming read for very large artifacts. The current design reads up to `limit` bytes; a 10 MB artifact still produces a 4 KB read. The §19.3 contract is bounded — the cost of "read everything" is a separate design.

## Forward references

- M3-T2 (Evaluation phase) lands this ADR's config + helper + prompt change in the same commit as the §19 evaluation rubric; it's the same size of change (a few lines) and lands in the same `internal/engine` review.
- M3-T3 (Comparison phase hardening) will exercise the new audit fields when it measures confidence calibration as part of its multi-record-synthesis work.
- M3-T7 (quality probe #2) will produce the empirical data to decide whether the default 4 KB is right for the 27B/9B model set.
