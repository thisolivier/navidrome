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

Run the playlist folder packages specifically:

```bash
go test -tags 'netgo,sqlite_fts5' ./persistence/... ./server/nativeapi/... ./model/...
```

## What Was Added

### Playlist Folders v2 (placement model)

A per-user organizational layer over playlists, implementing the semantics of
`docs/design/opensubsonic-playlist-folders-counterproposal.md` §3 on the native REST API.

- Folders nest arbitrarily deep (`parent_id`, `''` = root) and are private to their owner.
- A **placement** is `(user, playlist, folder-or-root, optional sortOrder)`. A playlist may
  appear in any number of folders, at most once per folder, and is ordered independently in
  each. A playlist with no placement is implicitly at the root, unordered.
- `sortOrder` is client-assigned, stored unnormalized, duplicates allowed; sibling folders
  and placements share one ordering space. `NULL` means unordered — clients sort, the server
  only stores.
- Deleting a folder is a pure schema operation: the folder row goes and everything it held
  lifts one level to the deleted folder's parent — children re-parent and placements are
  re-homed, carrying their sort order. If a user already has that playlist placed in the
  parent, their existing placement wins untouched. Deleting a root folder turns its
  placements into explicit root placements. Playlists are never touched.
- Re-parenting a folder under itself or one of its descendants is rejected with HTTP 400.
- Playlist responses carry no folder fields — `getPlaylists`/`/api/playlist` are unchanged.
- Placements follow the playlist's lifecycle: deleting a playlist removes every user's
  placements of it, and reads filter out any placement whose playlist is missing, so an
  orphan row can never reach a client.
- `SetPlacements` writes the new placements before pruning the superseded ones, so a failure
  part-way through can leave extra placements but never an emptied tree — these repositories
  run outside a transaction.

**Endpoints** (all under the native API, authenticated, scoped to the calling user):

| Method | Path | Semantics |
|--------|------|-----------|
| GET | `/playlist/folder` | v2 envelope: `{folderApiVersion, folders[], placements[]}` — two queries, no N+1 |
| POST | `/playlist/folder` | Create. Body `{name, parentId?, sortOrder?}` |
| PUT | `/playlist/folder/{id}` | Partial update `{name?, parentId?, sortOrder?}`; cycle detection |
| DELETE | `/playlist/folder/{id}` | Delete folder; children re-parent and placements re-home to its parent |
| PUT | `/playlist/folder/{id}/playlist/{playlistId}` | Create/update one placement. Body `{sortOrder?}` optional — the request may carry no body at all |
| DELETE | `/playlist/folder/{id}/playlist/{playlistId}` | Remove one placement (idempotent) |
| PUT | `/playlist/{playlistId}/placements` | Replace **all** placements of a playlist. Body `{placements:[{folderId, sortOrder?}]}`; empty list unfiles |

`{id}` accepts the reserved value `root` for the root of the tree; `folderId` `""` (or
`"root"`) in a body means the same. Unknown and foreign folder IDs are both 404 so trees
cannot be probed.

**Files:**

| File | Purpose |
|------|---------|
| `db/migrations/20260520000000_add_playlist_folders.go` | Creates `playlist_folder` and `playlist_placement` |
| `model/playlist_folder.go` | `PlaylistFolder`, `PlaylistPlacement`, `PlaylistFolderRepository` |
| `persistence/playlist_folder_repository.go` | Folder CRUD, owner scoping, releasing delete |
| `persistence/playlist_placement_repository.go` | Placement upsert / delete / set, user scoping |
| `persistence/playlist_folder_repository_test.go` | 33 Ginkgo specs (schema, ordering, deletion, lifecycle, isolation) |
| `server/nativeapi/playlist_folders.go` | Routes, tree envelope, folder handlers |
| `server/nativeapi/playlist_placements.go` | Placement handlers |
| `server/nativeapi/playlist_folder_support.go` | Response shapes, request decoding, cycle detection |
| `server/nativeapi/playlist_folder*_test.go`, `playlist_placements_test.go` | 36 Ginkgo specs + in-memory repository mock |

**Files modified:** `model/datastore.go` (`PlaylistFolder()` on `DataStore`),
`persistence/persistence.go` (factory + `Resource()` case), `tests/mock_data_store.go`,
`persistence/playlist_repository.go` (placement cleanup on playlist delete).

**Schema note:** foreign keys are declared as intent only. SQLite applies
`PRAGMA foreign_keys` per connection, so cascades are not relied upon — the repository issues
explicit deletes — in `playlistFolderRepository.Delete` (folder removal) and
`playlistRepository.Delete` (playlist removal). `playlist_placement.folder_id` carries no
foreign key at all, because `''` (root) is not a folder row.

### Song deletion

`DELETE /api/song/{id}` moves a track to the trash. See `server/nativeapi/song_delete.go`.

## Architecture Notes

- **Migrations**: Goose v3, Go functions in `db/migrations/`, registered via `init()` + `goose.AddMigrationContext()`
- **Models**: Struct-based in `model/`, using `structs` tags for DB columns and `json` tags for API
- **Repositories**: Interface in `model/`, implementation in `persistence/`, factory via `DataStore`
- **Query builder**: Masterminds/Squirrel on PocketBase/dbx on SQLite3 (mattn/go-sqlite3)
- **Tests**: Ginkgo v2 + Gomega, in-memory SQLite, shared fixtures in `persistence_suite_test.go`
- **ID generation**: `model/id.NewRandom()` for new entity IDs
