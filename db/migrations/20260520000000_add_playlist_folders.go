package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddPlaylistFolders, downAddPlaylistFolders)
}

// upAddPlaylistFolders creates the per-user playlist organization schema.
//
//   - playlist_folder holds the folder tree. parent_id is '' for a root folder.
//     sort_order is nullable: NULL means "unordered", which clients sort after ordered siblings.
//   - playlist_placement records that a playlist appears in a folder for one user.
//     folder_id is '' for an explicit root placement, so it cannot carry a foreign key.
//     A playlist with no placement rows is implicitly at the root, unordered.
//
// The foreign keys below document intent only. SQLite applies PRAGMA foreign_keys per
// connection, so cascades are not relied upon: the repository issues explicit deletes.
func upAddPlaylistFolders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS playlist_folder (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL,
			sort_order INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS playlist_folder_parent_id_idx ON playlist_folder(parent_id);
		CREATE INDEX IF NOT EXISTS playlist_folder_owner_id_idx ON playlist_folder(owner_id);

		CREATE TABLE IF NOT EXISTS playlist_placement (
			user_id TEXT NOT NULL,
			playlist_id TEXT NOT NULL REFERENCES playlist(id) ON DELETE CASCADE,
			folder_id TEXT NOT NULL DEFAULT '',
			sort_order INTEGER,
			PRIMARY KEY (user_id, playlist_id, folder_id)
		);
		CREATE INDEX IF NOT EXISTS playlist_placement_playlist_id_idx ON playlist_placement(playlist_id);
	`)
	return err
}

func downAddPlaylistFolders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS playlist_placement;
		DROP TABLE IF EXISTS playlist_folder;
	`)
	return err
}
