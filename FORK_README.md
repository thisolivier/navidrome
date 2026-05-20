# Navidrome Fork — musicLibrary

Local fork of [Navidrome](https://github.com/navidrome/navidrome) for implementing playlist folders, metadata editing, and song deletion features.

## Build

Requires Go 1.26+. Build tags are mandatory:

```bash
go build -tags 'netgo,sqlite_fts5' ./...
```

## Test

Run all tests:

```bash
go test -tags 'netgo,sqlite_fts5' ./...
```

Run playlist folder tests specifically:

```bash
go test -tags 'netgo,sqlite_fts5' -v ./persistence/ --ginkgo.v 2>&1 | grep -i "playlist.folder"
```

## What Was Added

### Playlist Folders (Sprint 038)

Organizational layer for grouping playlists into nested folders. A playlist can belong to multiple folders (many-to-many), and folders can be nested arbitrarily deep.

**Files added:**

| File | Purpose |
|------|---------|
| `db/migrations/20260520000000_add_playlist_folders.go` | Migration creating `playlist_folder` and `playlist_folder_membership` tables |
| `model/playlist_folder.go` | `PlaylistFolder` model struct and `PlaylistFolderRepository` interface |
| `persistence/playlist_folder_repository.go` | SQLite repository implementation with owner isolation |
| `persistence/playlist_folder_repository_test.go` | 27 Ginkgo tests covering CRUD, cascades, nesting, many-to-many, and owner isolation |

**Files modified:**

| File | Change |
|------|--------|
| `model/datastore.go` | Added `PlaylistFolder()` method to `DataStore` interface |
| `persistence/persistence.go` | Added `PlaylistFolder()` factory and `Resource()` switch case |
| `tests/mock_data_store.go` | Added mock `PlaylistFolder()` method |

## Architecture Notes

- **Migrations**: Goose v3, Go functions in `db/migrations/`, registered via `init()` + `goose.AddMigrationContext()`
- **Models**: Struct-based in `model/`, using `structs` tags for DB columns and `json` tags for API
- **Repositories**: Interface in `model/`, implementation in `persistence/`, factory via `DataStore`
- **Query builder**: Masterminds/Squirrel on PocketBase/dbx on SQLite3 (mattn/go-sqlite3)
- **Tests**: Ginkgo v2 + Gomega, in-memory SQLite, shared fixtures in `persistence_suite_test.go`
- **ID generation**: `model/id.NewRandom()` for new entity IDs
