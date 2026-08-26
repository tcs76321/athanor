package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tcs76321/athanor/migrations"
)

// openTemp is defined in store_test.go.

// TestFailedMigrationLeavesPriorVersionIntact proves the interrupted-
// migration guarantee: a migration failing mid-way rolls back entirely,
// the database stays at its prior version and remains fully usable, and a
// subsequent rerun neither corrupts nor double-applies anything. A kill -9
// mid-transaction exercises the same rollback path via WAL recovery.
func TestFailedMigrationLeavesPriorVersionIntact(t *testing.T) {
	s, _ := openTemp(t)
	db := s.DB()

	good := fstest.MapFS{
		"0001_create_thing.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE things (id TEXT PRIMARY KEY, val TEXT NOT NULL);"),
		},
	}
	if err := Migrate(db, good, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO things VALUES ('t1','hello')`); err != nil {
		t.Fatal(err)
	}

	broken := fstest.MapFS{
		"0001_create_thing.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE things (id TEXT PRIMARY KEY, val TEXT NOT NULL);"),
		},
		"0002_add_stuff.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE stuff (id TEXT);\nINSERT INTO nonexistent_table VALUES (1);"),
		},
	}
	err := Migrate(db, broken, "")
	if err == nil {
		t.Fatal("broken migration succeeded, want failure")
	}
	if !strings.Contains(err.Error(), "remains at version 1") {
		t.Errorf("error should report surviving version: %v", err)
	}

	if got := VersionOf(t, db); got != 1 {
		t.Fatalf("version = %d after failed migration, want 1", got)
	}
	var val string
	if err := db.QueryRow(`SELECT val FROM things WHERE id='t1'`).Scan(&val); err != nil || val != "hello" {
		t.Fatalf("pre-existing data lost or wrong: val=%q err=%v", val, err)
	}
	var stuff int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='stuff'`,
	).Scan(&stuff); err != nil {
		t.Fatalf("checking for leaked 'stuff' table: %v", err)
	}
	if stuff != 0 {
		t.Fatalf("partial migration leaked table 'stuff' (count=%d)", stuff)
	}

	// Rerunning the same failing set fails again but changes nothing.
	if err := Migrate(db, broken, ""); err == nil {
		t.Fatal("second run of broken set succeeded")
	}
	if got := VersionOf(t, db); got != 1 {
		t.Fatalf("version = %d after failed rerun, want 1", got)
	}

	// Rerunning only applied migrations is a no-op success.
	if err := Migrate(db, good, ""); err != nil {
		t.Fatalf("no-op rerun failed: %v", err)
	}
}

func TestDuplicateMigrationVersionsRejected(t *testing.T) {
	s, _ := openTemp(t)
	fs := fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id TEXT);")},
		"0001_b.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id TEXT);")},
	}
	if err := Migrate(s.DB(), fs, ""); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v, want duplicate version error", err)
	}
}

func TestBadMigrationNamesRejected(t *testing.T) {
	s, _ := openTemp(t)
	fs := fstest.MapFS{
		"create_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id TEXT);")},
	}
	if err := Migrate(s.DB(), fs, ""); err == nil || !strings.Contains(err.Error(), "NNNN_description.sql") {
		t.Fatalf("err = %v, want naming error", err)
	}
}

func TestEventsTableIsAppendOnly(t *testing.T) {
	s, _ := openTemp(t)
	if err := Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	if _, err := db.Exec(
		`INSERT INTO events (category, data_json) VALUES ('jobs', '{"x":1}')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET category='hacked'`); err == nil {
		t.Error("UPDATE on events accepted")
	}
	if _, err := db.Exec(`DELETE FROM events`); err == nil {
		t.Error("DELETE on events accepted")
	}
}

// TestNoEventsMutationInSource guards the invariant that this package's
// SQL never mutates the append-only events table. Only production sources
// are scanned (test fixtures legitimately contain such strings).
func TestNoEventsMutationInSource(t *testing.T) {
	files, err := filepath.Glob("./*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			t.Fatal(readErr)
		}
		src := strings.ToUpper(string(data))
		if strings.Contains(src, "UPDATE"+" EVENTS") || strings.Contains(src, "DELETE FROM"+" EVENTS") {
			t.Errorf("%s contains an events mutation statement", f)
		}
	}
}

func VersionOf(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
