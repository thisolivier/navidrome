package persistence

import (
	"context"
	"errors"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type playlistFolderRepository struct {
	sqlRepository
}

func NewPlaylistFolderRepository(ctx context.Context, db dbx.Builder) model.PlaylistFolderRepository {
	r := &playlistFolderRepository{}
	r.ctx = ctx
	r.db = db
	r.registerModel(&model.PlaylistFolder{}, nil)
	return r
}

func (r *playlistFolderRepository) ownerFilter() Sqlizer {
	user := loggedUser(r.ctx)
	if user.IsAdmin {
		return And{}
	}
	return Eq{"owner_id": user.ID}
}

func (r *playlistFolderRepository) Put(folder *model.PlaylistFolder) error {
	if folder.OwnerID == "" {
		folder.OwnerID = loggedUser(r.ctx).ID
	}
	if folder.ID == "" {
		folder.CreatedAt = time.Now()
	}
	folder.UpdatedAt = time.Now()

	id, err := r.put(folder.ID, folder)
	if err != nil {
		return err
	}
	folder.ID = id
	return nil
}

func (r *playlistFolderRepository) Get(id string) (*model.PlaylistFolder, error) {
	sel := r.newSelect().Columns("*").Where(And{Eq{"id": id}, r.ownerFilter()})
	var folder model.PlaylistFolder
	err := r.queryOne(sel, &folder)
	if err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *playlistFolderRepository) GetAll(options ...model.QueryOptions) (model.PlaylistFolders, error) {
	sel := r.newSelect(options...).Columns("*").Where(r.ownerFilter())
	var folders model.PlaylistFolders
	err := r.queryAll(sel, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *playlistFolderRepository) GetRoots(options ...model.QueryOptions) (model.PlaylistFolders, error) {
	sel := r.newSelect(options...).Columns("*").Where(And{Eq{"parent_id": ""}, r.ownerFilter()})
	var folders model.PlaylistFolders
	err := r.queryAll(sel, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *playlistFolderRepository) GetChildren(parentID string) (model.PlaylistFolders, error) {
	sel := r.newSelect().Columns("*").Where(And{Eq{"parent_id": parentID}, r.ownerFilter()})
	var folders model.PlaylistFolders
	err := r.queryAll(sel, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *playlistFolderRepository) Delete(id string) error {
	// Check ownership up front. delete() reports no error when it matches zero
	// rows, so it cannot guard the cleanup below: without this, a user could
	// re-parent another user's child folders by deleting a folder they do not own.
	if _, err := r.Get(id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			// Missing, or not visible to this caller: both are reported as "not
			// found" so the endpoint never confirms the existence of another
			// user's folder. Translated to rest.ErrNotFound because the REST
			// controller matches that value by identity to return 404; the model
			// error would fall through to a 500. Same approach as playerRepository.
			return rest.ErrNotFound
		}
		return err
	}

	// Promote children to root rather than deleting the subtree
	promote := Update(r.tableName).Set("parent_id", "").Where(Eq{"parent_id": id})
	if _, err := r.executeSQL(promote); err != nil {
		return err
	}

	// Drop this folder's playlist memberships explicitly. The schema declares
	// ON DELETE CASCADE, but SQLite enforces foreign keys per connection and the
	// pragma is not applied to every connection in the pool, so the cascade
	// cannot be relied on at runtime. Leaving the rows behind would strand a
	// playlist: still holding a membership, but to a folder that no longer exists.
	clearMemberships := Delete("playlist_folder_membership").Where(Eq{"folder_id": id})
	if _, err := r.executeSQL(clearMemberships); err != nil {
		return err
	}

	return r.delete(And{Eq{"id": id}, r.ownerFilter()})
}

func (r *playlistFolderRepository) AddPlaylist(folderID, playlistID string) error {
	ins := Insert("playlist_folder_membership").
		Columns("folder_id", "playlist_id").
		Values(folderID, playlistID)
	_, err := r.executeSQL(ins)
	return err
}

func (r *playlistFolderRepository) RemovePlaylist(folderID, playlistID string) error {
	del := Delete("playlist_folder_membership").
		Where(And{Eq{"folder_id": folderID}, Eq{"playlist_id": playlistID}})
	_, err := r.executeSQL(del)
	return err
}

func (r *playlistFolderRepository) GetPlaylistFolders(playlistID string) (model.PlaylistFolders, error) {
	sel := Select(r.tableName + ".*").
		From(r.tableName).
		Join("playlist_folder_membership ON playlist_folder_membership.folder_id = " + r.tableName + ".id").
		Where(And{Eq{"playlist_folder_membership.playlist_id": playlistID}, r.ownerFilter()})
	var folders model.PlaylistFolders
	err := r.queryAll(sel, &folders)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *playlistFolderRepository) GetPlaylistIDs(folderID string) ([]string, error) {
	sel := Select("playlist_id").
		From("playlist_folder_membership").
		Where(Eq{"folder_id": folderID})
	var ids []string
	err := r.queryAllSlice(sel, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ResourceRepository (rest.Repository) implementation

func (r *playlistFolderRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.count(Select().Where(r.ownerFilter()), r.parseRestOptions(r.ctx, options...))
}

func (r *playlistFolderRepository) Read(id string) (any, error) {
	return r.Get(id)
}

func (r *playlistFolderRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

func (r *playlistFolderRepository) EntityName() string {
	return "playlist_folder"
}

func (r *playlistFolderRepository) NewInstance() any {
	return &model.PlaylistFolder{}
}

// rest.Persistable implementation

func (r *playlistFolderRepository) Save(entity any) (string, error) {
	folder := entity.(*model.PlaylistFolder)
	err := r.Put(folder)
	if err != nil {
		return "", err
	}
	return folder.ID, nil
}

func (r *playlistFolderRepository) Update(id string, entity any, cols ...string) error {
	folder := entity.(*model.PlaylistFolder)
	folder.ID = id
	return r.Put(folder)
}

var _ rest.Persistable = (*playlistFolderRepository)(nil)
var _ model.PlaylistFolderRepository = (*playlistFolderRepository)(nil)
