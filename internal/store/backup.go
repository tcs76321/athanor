package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Backup writes a consistent snapshot of the database to
// <backupDir>/athanor-v<version>-<timestamp>.db using VACUUM INTO, which
// is atomic and safe against concurrent writers. Returns the backup path.
func Backup(db *sql.DB, backupDir string, version int) (string, error) {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("creating backup dir: %w", err)
	}
	name := fmt.Sprintf("athanor-v%04d-%s.db", version, time.Now().UTC().Format("20060102T150405Z"))
	dest := filepath.Join(backupDir, name)
	// VACUUM INTO takes a literal expression; bind parameters are not
	// reliably supported for it, so quote the path defensively.
	q := strings.ReplaceAll(dest, "'", "''")
	if _, err := db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, q)); err != nil {
		return "", fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	return dest, nil
}
