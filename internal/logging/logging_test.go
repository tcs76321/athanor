package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcs76321/athanor/internal/config"
)

func TestCategoriesEmitDistinctTaggedEvents(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{Dir: dir, Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	for _, cat := range config.Categories {
		log, err := m.Logger(cat)
		if err != nil {
			t.Fatalf("Logger(%q): %v", cat, err)
		}
		log.Info("test event", "seq", 1)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{}
	for _, e := range entries {
		files[e.Name()] = true
	}
	for _, cat := range config.Categories {
		name := cat + ".log"
		if !files[name] {
			t.Fatalf("expected log file %q; have %v", name, files)
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		if !scanner.Scan() {
			t.Fatalf("%s: no events written", name)
		}
		var rec struct {
			Category string `json:"category"`
			Msg      string `json:"msg"`
			Level    string `json:"level"`
			Time     string `json:"time"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("%s: event is not valid JSON: %v", name, err)
		}
		if rec.Category != cat {
			t.Errorf("%s: category tag = %q", name, rec.Category)
		}
		if rec.Msg != "test event" || rec.Level == "" || rec.Time == "" {
			t.Errorf("%s: incomplete event record: %+v", name, rec)
		}
		f.Close()
	}
}

func TestDisabledCategoryRejected(t *testing.T) {
	m, err := New(Options{Dir: t.TempDir(), Categories: []string{"jobs"}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Logger("jobs"); err != nil {
		t.Fatalf("enabled category rejected: %v", err)
	}
	if _, err := m.Logger("airlock"); err == nil {
		t.Fatal("disabled category accepted")
	}
}

func TestRotationAndPruning(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{
		Dir:          dir,
		Level:        "info",
		MaxSizeBytes: 200,
		KeepFiles:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	log, err := m.Logger("jobs")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 80)
	for i := 0; i < 30; i++ { // ~2.4KB total forces several rotations
		log.Info(line)
	}

	active := filepath.Join(dir, "jobs.log")
	info, err := os.Stat(active)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 200+200 { // active file stays near the threshold
		t.Errorf("active log size = %d, rotation not bounding size", info.Size())
	}
	matches, _ := filepath.Glob(active + ".*")
	if len(matches) > 2 {
		t.Errorf("retained %d rotated files, want <= 2", len(matches))
	}
	if len(matches) < 1 {
		t.Error("no rotated files found — rotation never happened")
	}
}

func TestLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{Dir: dir, Level: "warn"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	log, err := m.Logger("alarms")
	if err != nil {
		t.Fatal(err)
	}
	log.Info("should not appear")
	log.Warn("should appear")
	data, err := os.ReadFile(filepath.Join(dir, "alarms.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "should not appear") {
		t.Error("info event leaked through warn level")
	}
	if !strings.Contains(content, "should appear") {
		t.Error("warn event missing")
	}
}

func TestUnknownLevelRejected(t *testing.T) {
	if _, err := New(Options{Dir: t.TempDir(), Level: "loud"}); err == nil {
		t.Fatal("unknown level accepted")
	}
}

func TestClosedManagerRejectsNewLoggers(t *testing.T) {
	m, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Logger("jobs"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Logger("jobs"); err == nil {
		t.Fatal("logger created after Close")
	}
	if err := m.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}
