// modernc.org/sqlite spike (pure Go, no CGO)
// Run: CGO_ENABLED=0 go run main_modernc.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
)

func main() {
	fmt.Println("=== modernc.org/sqlite (pure Go) ===")
	ctx := context.Background()
	testDriver(ctx, "modernc", findExtension())
}

func testDriver(ctx context.Context, name, extPath string) {
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Extension: %s\n", extPath)

	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		log.Fatalf("Extension not found: %s", extPath)
	}

	dbPath := "test_modernc.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_fk=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("[%s] Open: %v", name, err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// modernc: try load_extension (likely fails)
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatalf("[%s] Conn: %v", name, err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SELECT load_extension('%s')", extPath)); err != nil {
		log.Printf("[%s] ⚠️  load_extension failed (expected): %v", name, err)
		fmt.Printf("[%s] modernc cannot load dynamic .so/.dylib extensions at runtime.\n", name)
		fmt.Printf("[%s] Would require custom Wasm build with sqlite-vec baked in.\n", name)
		
		// Still test FTS5 as fallback
		if err := testFTS5(ctx, conn, name); err != nil {
			log.Printf("[%s] FTS5 test failed: %v", name, err)
		} else {
			fmt.Printf("[%s] ✅ FTS5 works (fallback)\n", name)
		}
		return
	}

	// If somehow it works, test both
	fmt.Printf("[%s] ✅ sqlite-vec loaded (unexpected!)\n", name)
	testVec0(ctx, conn, name)
	testFTS5(ctx, conn, name)
}

func testVec0(ctx context.Context, conn *sql.Conn, name string) error {
	// Same implementation as main.go
	if _, err := conn.ExecContext(ctx, `CREATE VIRTUAL TABLE vec_items USING vec0(embedding float[4])`); err != nil {
		return err
	}
	vectors := [][]float32{{0.1, 0.2, 0.3, 0.4}, {0.5, 0.6, 0.7, 0.8}, {-0.1, -0.2, -0.3, -0.4}, {0.9, 0.8, 0.7, 0.6}}
	for i, v := range vectors {
		vecJSON, err := json.Marshal(v)
		if err != nil {
			log.Printf("[%s] json.Marshal failed: %v", name, err)
			return err
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO vec_items(rowid, embedding) VALUES (?, ?)", int64(i+1), string(vecJSON)); err != nil {
			return err
		}
	}
	queryVec := []float32{0.15, 0.25, 0.35, 0.45}
	queryJSON, err := json.Marshal(queryVec)
	if err != nil {
		log.Printf("[%s] json.Marshal failed: %v", name, err)
		return err
	}
	rows, err := conn.QueryContext(ctx, `SELECT rowid, distance FROM vec_items WHERE embedding MATCH ? ORDER BY distance LIMIT 3`, string(queryJSON))
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Printf("[%s] KNN Results:\n", name)
	for rows.Next() {
		var rowid int64
		var dist float64
		if err := rows.Scan(&rowid, &dist); err != nil {
			log.Printf("[%s] rows.Scan failed: %v", name, err)
			return err
		}
		fmt.Printf("  rowid=%d distance=%.6f\n", rowid, dist)
	}
	return rows.Err()
}

func testFTS5(ctx context.Context, conn *sql.Conn, name string) error {
	if _, err := conn.ExecContext(ctx, `CREATE VIRTUAL TABLE fts_items USING fts5(content, title)`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fts_items(rowid, content, title) VALUES (1, 'AI agent architecture design', 'Design doc')`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fts_items(rowid, content, title) VALUES (2, 'Vector search with sqlite-vec', 'Tech note')`); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, `SELECT rowid, rank FROM fts_items WHERE fts_items MATCH 'agent' ORDER BY rank`)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Printf("[%s] FTS5 Results:\n", name)
	for rows.Next() {
		var rowid int64
		var rank float64
		if err := rows.Scan(&rowid, &rank); err != nil {
			log.Printf("[%s] rows.Scan failed: %v", name, err)
			return err
		}
		fmt.Printf("  rowid=%d rank=%.6f\n", rowid, rank)
	}
	return rows.Err()
}

// findExtension searches the build/ directory for a valid sqlite-vec extension
// matching the current platform. Returns the first match found.
func findExtension() string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"build/vec.dylib",
			"build/vec0.dylib",
			"build/sqlite-vec.dylib",
		}
	case "linux":
		candidates = []string{
			"build/sqlite-vec.so",
			"build/vec0.so",
			"build/vec.so",
		}
	default:
		log.Fatalf("Unsupported OS: %s", runtime.GOOS)
	}

	for _, candidate := range candidates {
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	log.Fatalf("No sqlite-vec extension found in build/ for %s/%s. Candidates: %v", runtime.GOOS, runtime.GOARCH, candidates)
	return ""
}