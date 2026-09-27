package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

// playlistPlacementTableName holds the (user, playlist, folder) placement rows. See
// playlist_placement_repository.go for the placement half of this repository.
const playlistPlacementTableName = "playlist_placement"

type playlistFolderRepository struct {
	sqlRepository
}

func NewPlaylistFolderRepository(db dbx.Builder) model.PlaylistFolderRepository {
	repository := &playlistFolderRepository{}
	repository.db = db
	repository.registerModel(&model.PlaylistFolder{}, nil)
	return repository
}

// currentUserID is the owner of every folder and placement this repository touches.
func (r *playlistFolderRepository) currentUserID(ctx context.Context) string {
	return loggedUser(ctx).ID
}

// folderOwnerFilter scopes folders to the calling user. Folder trees are private per-user
// organization data, so there is deliberately no administrator bypass here.
func (r *playlistFolderRepository) folderOwnerFilter(ctx context.Context) Sqlizer {
	return Eq{"owner_id": r.currentUserID(ctx)}
}

func (r *playlistFolderRepository) Put(ctx context.Context, folder *model.PlaylistFolder) error {
	if folder.OwnerID == "" {
		folder.OwnerID = r.currentUserID(ctx)
	}
	if folder.ID == "" {
		folder.CreatedAt = time.Now()
	}
	folder.UpdatedAt = time.Now()

	newID, err := r.put(ctx, folder.ID, folder)
	if err != nil {
		return err
	}
	folder.ID = newID
	return nil
}

func (r *playlistFolderRepository) Get(ctx context.Context, id string) (*model.PlaylistFolder, error) {
	selectFolder := r.newSelect(ctx).Columns("*").Where(And{Eq{"id": id}, r.folderOwnerFilter(ctx)})
	var folder model.PlaylistFolder
	err := r.queryOne(ctx, selectFolder, &folder)
	if err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *playlistFolderRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.PlaylistFolders, error) {
	selectFolders := r.newSelect(ctx, options...).Columns("*").Where(r.folderOwnerFilter(ctx))
	folders := model.PlaylistFolders{}
	err := r.queryAll(ctx, selectFolders, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *playlistFolderRepository) GetChildren(ctx context.Context, parentID string) (model.PlaylistFolders, error) {
	selectFolders := r.newSelect(ctx).Columns("*").Where(And{Eq{"parent_id": parentID}, r.folderOwnerFilter(ctx)})
	folders := model.PlaylistFolders{}
	err := r.queryAll(ctx, selectFolders, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

// Delete removes a folder as a pure schema operation: everything the folder held moves up
// one level to the folder's own parent, and playlists are untouched. Children re-parent and
// placements are re-homed to the same parent, so contents lift together instead of the
// folders lifting while the playlists drop to the root. When the deleted folder was itself
// at the root, its placements become explicit root placements and keep their order.
//
// The steps are ordered so nothing is lost if a later one fails: the promoted placements are
// written before the old rows are removed. Placement deletes are explicit because SQLite only
// enforces foreign keys on connections that enabled PRAGMA foreign_keys.
func (r *playlistFolderRepository) Delete(ctx context.Context, id string) error {
	folder, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	reparentChildren := Update(r.tableName).
		Set("parent_id", folder.ParentID).
		Set("updated_at", time.Now()).
		Where(And{Eq{"parent_id": id}, r.folderOwnerFilter(ctx)})
	if _, err = r.executeSQL(ctx, reparentChildren); err != nil {
		return err
	}

	if err = r.promotePlacements(ctx, id, folder.ParentID); err != nil {
		return err
	}

	removeOldPlacements := Delete(playlistPlacementTableName).Where(Eq{"folder_id": id})
	if _, err = r.executeSQL(ctx, removeOldPlacements); err != nil {
		return err
	}

	return r.delete(ctx, And{Eq{"id": id}, r.folderOwnerFilter(ctx)})
}

// promotePlacements copies every placement of a folder into the folder's parent, carrying
// the sort order over as-is. A user that already has the playlist placed in the parent keeps
// that placement untouched — the existing home wins, so nothing a user arranged is
// overwritten. Duplicate sort orders among the new siblings are legal.
func (r *playlistFolderRepository) promotePlacements(ctx context.Context, folderID, parentID string) error {
	selectPromoted := Select("user_id", "playlist_id").
		Column("?", parentID).
		Column("sort_order").
		From(playlistPlacementTableName).
		Where(Eq{"folder_id": folderID})

	promotePlacements := Insert(playlistPlacementTableName).
		Columns(placementColumns...).
		Select(selectPromoted).
		Suffix("on conflict (user_id, playlist_id, folder_id) do nothing")

	_, err := r.executeSQL(ctx, promotePlacements)
	return err
}

var _ model.PlaylistFolderRepository = (*playlistFolderRepository)(nil)
