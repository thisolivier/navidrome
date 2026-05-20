package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddPlaylistFolders, downAddPlaylistFolders)
}

func upAddPlaylistFolders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS playlist_folder (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS playlist_folder_membership (
			folder_id TEXT NOT NULL REFERENCES playlist_folder(id) ON DELETE CASCADE,
			playlist_id TEXT NOT NULL REFERENCES playlist(id) ON DELETE CASCADE,
			PRIMARY KEY (folder_id, playlist_id)
		);
	`)
	return err
}

func downAddPlaylistFolders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS playlist_folder_membership;
		DROP TABLE IF EXISTS playlist_folder;
	`)
	return err
}
