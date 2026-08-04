package persistence

import (
	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

// Placement half of playlistFolderRepository. A placement is (user, playlist, folder,
// optional sort order), with folder_id '' meaning the root of the user's tree. Placements
// are always scoped to the user in the request context: organizing a playlist never
// modifies the playlist, so a user may place any playlist visible to them.

var placementColumns = []string{"user_id", "playlist_id", "folder_id", "sort_order"}

func (r *playlistFolderRepository) GetPlacements() (model.PlaylistPlacements, error) {
	selectPlacements := Select(placementColumns...).
		From(playlistPlacementTableName).
		Where(Eq{"user_id": r.currentUserID()})
	placements := model.PlaylistPlacements{}
	err := r.queryAll(selectPlacements, &placements)
	if err != nil {
		return nil, err
	}
	return placements, nil
}

func (r *playlistFolderRepository) PutPlacement(placement model.PlaylistPlacement) error {
	_, err := r.executeSQL(r.insertPlacement(placement))
	return err
}

func (r *playlistFolderRepository) DeletePlacement(playlistID, folderID string) error {
	deletePlacement := Delete(playlistPlacementTableName).Where(And{
		Eq{"user_id": r.currentUserID()},
		Eq{"playlist_id": playlistID},
		Eq{"folder_id": folderID},
	})
	_, err := r.executeSQL(deletePlacement)
	return err
}

// SetPlacements makes the supplied list the complete set of placements this user has for
// the playlist. An empty list returns the playlist to the implicit root.
func (r *playlistFolderRepository) SetPlacements(playlistID string, placements model.PlaylistPlacements) error {
	deleteExisting := Delete(playlistPlacementTableName).Where(And{
		Eq{"user_id": r.currentUserID()},
		Eq{"playlist_id": playlistID},
	})
	if _, err := r.executeSQL(deleteExisting); err != nil {
		return err
	}
	for _, placement := range placements {
		placement.PlaylistID = playlistID
		if _, err := r.executeSQL(r.insertPlacement(placement)); err != nil {
			return err
		}
	}
	return nil
}

// insertPlacement upserts a single placement row for the current user.
func (r *playlistFolderRepository) insertPlacement(placement model.PlaylistPlacement) InsertBuilder {
	return Insert(playlistPlacementTableName).
		Columns(placementColumns...).
		Values(r.currentUserID(), placement.PlaylistID, placement.FolderID, placement.SortOrder).
		Suffix("on conflict (user_id, playlist_id, folder_id) do update set sort_order = excluded.sort_order")
}
