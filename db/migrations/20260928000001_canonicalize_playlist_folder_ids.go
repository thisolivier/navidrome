package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCanonicalizePlaylistFolderIds, downCanonicalizePlaylistFolderIds)
}

// upCanonicalizePlaylistFolderIds re-encodes the id-bearing columns of the fork's playlist
// organization tables with the canonical 22-char base62 id shape.
//
// Upstream's 20260720015443_uniform_canonical_ids rewrote playlist.id and user.id (among
// others), but playlist_folder and playlist_placement are fork-only tables absent from its
// idColumns list, so deployments that predate this rebase still hold pre-canonical ids in
// them. canonicalID is deterministic, so applying it here yields exactly the values the
// upstream migration wrote into playlist.id/user.id, relinking every placement and folder.
//
// Empty strings pass through canonicalID unchanged, which preserves the reserved meanings:
// folder_id ” is a root placement and parent_id ” is a root folder. Unrecognized shapes
// (already-canonical ids included) also pass through, so the migration is idempotent.
//
// Fresh installs create the tables (empty) in the migration just before this one, so the
// rewrite is a no-op there; the table-existence guard additionally covers databases that
// somehow run this migration without the tables present.
func upCanonicalizePlaylistFolderIds(ctx context.Context, tx *sql.Tx) error {
	canonicalize := func(value string) (string, bool) {
		newValue := canonicalID(value)
		return newValue, newValue != value
	}
	folderColumns := []struct{ table, col string }{
		{"playlist_folder", "id"},
		{"playlist_folder", "parent_id"},
		{"playlist_folder", "owner_id"},
		{"playlist_placement", "user_id"},
		{"playlist_placement", "playlist_id"},
		{"playlist_placement", "folder_id"},
	}
	for _, tc := range folderColumns {
		exists, err := tableExists(ctx, tx, tc.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := rewriteColumn(ctx, tx, tc.table, tc.col, canonicalize); err != nil {
			return fmt.Errorf("canonicalizing %s.%s: %w", tc.table, tc.col, err)
		}
	}
	return nil
}

func tableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func downCanonicalizePlaylistFolderIds(ctx context.Context, tx *sql.Tx) error {
	// Irreversible in general: canonicalID does not retain the original encoding.
	return nil
}
