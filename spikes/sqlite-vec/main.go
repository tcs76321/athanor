// mattn/go-sqlite3 spike (CGO)
// Run: CGO_ENABLED=1 go run main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("=== mattn/go-sqlite3 (CGO) ===")
	ctx := context.Background()
	testDriver(ctx, "mattn", getExtensionPath())
}

func testDriver(ctx context.Context, name, extPath string) {
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Extension: %s\n", extPath)

	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		log.Fatalf("Extension not found: %s", extPath)
	}

	dbPath := "test_mattn.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		log.Fatalf("[%s] Open: %v", name, err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// Get a dedicated connection and use LoadExtension method
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatalf("[%s] Conn: %v", name, err)
	}
	defer conn.Close()

	// Use the driver's LoadExtension method
	var loader interface{ LoadExtension(string, string) error }
	if err := conn.Raw(func(dc any) error {
		var ok bool
		loader, ok = dc.(interface{ LoadExtension(string, string) error })
		if !ok {
			return fmt.Errorf("driver doesn't support LoadExtension")
		}
		return nil
	}); err != nil {
		log.Fatalf("[%s] Raw: %v", name, err)
	}

	// sqlite-vec uses sqlite3_vec_init as entry point
	if err := loader.LoadExtension(extPath, "sqlite3_vec_init"); err != nil {
		log.Fatalf("[%s] LoadExtension: %v", name, err)
	}
	fmt.Printf("[%s] ✅ sqlite-vec loaded\n", name)

	// CRITICAL: Extensions are per-connection! We must use this same connection for all operations.
	// Run all tests on THIS connection.
	fmt.Printf("[%s] Testing vec0...\n", name)
	if err := testVec0(ctx, conn, name); err != nil {
		log.Printf("[%s] vec0 test failed: %v", name, err)
	} else {
		fmt.Printf("[%s] ✅ vec0 KNN works\n", name)
	}

	fmt.Printf("[%s] Testing FTS5...\n", name)
	if err := testFTS5(ctx, conn, name); err != nil {
		log.Printf("[%s] FTS5 test failed: %v", name, err)
	} else {
		fmt.Printf("[%s] ✅ FTS5 works\n", name)
	}
}

func testVec0(ctx context.Context, conn *sql.Conn, name string) error {
	fmt.Printf("[%s] Creating vec0 table...\n", name)
	if _, err := conn.ExecContext(ctx, `CREATE VIRTUAL TABLE vec_items USING vec0(embedding float[4])`); err != nil {
		return err
	}

	fmt.Printf("[%s] Inserting vectors...\n", name)
	vectors := [][]float32{
		{0.1, 0.2, 0.3, 0.4},
		{0.5, 0.6, 0.7, 0.8},
		{-0.1, -0.2, -0.3, -0.4},
		{0.9, 0.8, 0.7, 0.6},
	}
	for i, v := range vectors {
		vecJSON, _ := json.Marshal(v)
		if _, err := conn.ExecContext(ctx, "INSERT INTO vec_items(rowid, embedding) VALUES (?, ?)", int64(i+1), string(vecJSON)); err != nil {
			return err
		}
	}

	fmt.Printf("[%s] Running KNN query...\n", name)
	queryVec := []float32{0.15, 0.25, 0.35, 0.45}
	queryJSON, _ := json.Marshal(queryVec)

	// vec0 requires k = ? parameter for KNN queries
	rows, err := conn.QueryContext(ctx, `
		SELECT rowid, distance
		FROM vec_items
		WHERE embedding MATCH ? AND k = 3
		ORDER BY distance
	`, string(queryJSON))
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Printf("[%s] KNN Results:\n", name)
	for rows.Next() {
		var rowid int64
		var dist float64
		rows.Scan(&rowid, &dist)
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
		rows.Scan(&rowid, &rank)
		fmt.Printf("  rowid=%d rank=%.6f\n", rowid, rank)
	}
	return rows.Err()
}

func getExtensionPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "build/vec.dylib"
	case "linux":
		return "build/sqlite-vec.so"
	default:
		log.Fatalf("Unsupported OS: %s", runtime.GOOS)
		return ""
	}
}