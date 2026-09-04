// Tests for the in-tree scanners shipped in M4-T2:
// `size` and `zipbomb`. The `prompt-injection-heuristic`,
// `clamav`, and `yara` scanners land in M4-T3.
package scanner

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSize_AcceptsUnderLimit(t *testing.T) {
	s := NewSize(1024)
	res, err := s.Scan(context.Background(), ScanInput{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean for in-limit size", res.Verdict)
	}
}

func TestSize_AcceptsAtLimit(t *testing.T) {
	s := NewSize(1024)
	res, _ := s.Scan(context.Background(), ScanInput{Size: 1024})
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean at exact limit", res.Verdict)
	}
}

func TestSize_RejectsOverLimit(t *testing.T) {
	s := NewSize(1024)
	res, _ := s.Scan(context.Background(), ScanInput{Size: 1025})
	if res.Verdict != VerdictRejected {
		t.Errorf("Verdict = %v, want VerdictRejected for over-limit size", res.Verdict)
	}
	if !strings.Contains(res.Reason, "exceeds_max") {
		t.Errorf("Reason = %q, want it to contain 'exceeds_max'", res.Reason)
	}
}

func TestSize_Name(t *testing.T) {
	if NewSize(1).Name() != "size" {
		t.Errorf("Size.Name() = %q, want \"size\"", NewSize(1).Name())
	}
}

func TestZipBomb_NonZipIsClean(t *testing.T) {
	z := NewZipBomb(100, 10000, 50)
	res, err := z.Scan(context.Background(), ScanInput{Bytes: []byte("not a zip")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean for non-zip input", res.Verdict)
	}
}

func TestZipBomb_NilInputIsClean(t *testing.T) {
	z := NewZipBomb(100, 10000, 50)
	res, err := z.Scan(context.Background(), ScanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean for nil input", res.Verdict)
	}
}

func TestZipBomb_NormalZipIsClean(t *testing.T) {
	// Build a small zip with a couple of small text
	// entries. Compressed/uncompressed ratio is well
	// under 100×; entry count is well under 10k.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte(strings.Repeat("hello\n", 10)))
	}
	_ = w.Close()
	z := NewZipBomb(100, 10000, 50)
	res, err := z.Scan(context.Background(), ScanInput{Bytes: buf.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean for normal zip; Reason=%q",
			res.Verdict, res.Reason)
	}
}

func TestZipBomb_TooManyEntries(t *testing.T) {
	// Build a zip with 11 entries; MaxEntries=10.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := 0; i < 11; i++ {
		f, _ := w.Create("f")
		_, _ = f.Write([]byte("x"))
	}
	_ = w.Close()
	z := NewZipBomb(100, 10, 50)
	res, _ := z.Scan(context.Background(), ScanInput{Bytes: buf.Bytes()})
	if res.Verdict != VerdictRejected {
		t.Errorf("Verdict = %v, want VerdictRejected for too-many-entries", res.Verdict)
	}
	if !strings.Contains(res.Reason, "too_many_entries") {
		t.Errorf("Reason = %q, want it to contain 'too_many_entries'", res.Reason)
	}
}

func TestZipBomb_Name(t *testing.T) {
	if NewZipBomb(1, 1, 1).Name() != "zipbomb" {
		t.Errorf("ZipBomb.Name() = %q, want \"zipbomb\"", NewZipBomb(1, 1, 1).Name())
	}
}
