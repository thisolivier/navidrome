package nativeapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
)

// putPlaylistPlacement creates the placement or updates its sort order. Placements belong
// to the calling user, and organizing never modifies the playlist, so a playlist the user
// can see but does not own may be placed freely.
func putPlaylistPlacement(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := resolveFolderIDFromPath(chi.URLParam(r, "id"))
		playlistID := chi.URLParam(r, "playlistId")

		var request placementWriteRequest
		if _, err := decodeJSONBody(r, &request); err != nil {
			respondBadRequest(w, "invalid request body")
			return
		}

		ctx := r.Context()
		repo := ds.PlaylistFolder()
		if !folderExistsForUser(ctx, w, repo, folderID) {
			return
		}

		placement := model.PlaylistPlacement{
			PlaylistID: playlistID,
			FolderID:   folderID,
			SortOrder:  request.SortOrder,
		}
		if err := repo.PutPlacement(ctx, placement); err != nil {
			respondServerError(w, err)
			return
		}
		respondWithJSON(w, newPlaylistPlacementResponse(placement))
	}
}

// deletePlaylistPlacement is idempotent: removing a placement that does not exist succeeds.
func deletePlaylistPlacement(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := resolveFolderIDFromPath(chi.URLParam(r, "id"))
		playlistID := chi.URLParam(r, "playlistId")

		ctx := r.Context()
		repo := ds.PlaylistFolder()
		if !folderExistsForUser(ctx, w, repo, folderID) {
			return
		}

		if err := repo.DeletePlacement(ctx, playlistID, folderID); err != nil {
			respondServerError(w, err)
			return
		}
		respondWithJSON(w, newPlaylistPlacementResponse(model.PlaylistPlacement{
			PlaylistID: playlistID,
			FolderID:   folderID,
		}))
	}
}

// setPlaylistPlacements replaces every placement this user has for the playlist with the
// supplied list — the "move" verb. An empty list unfiles the playlist back to the implicit
// root. Each item's folderId may be "" or the reserved "root" value for a root placement.
func setPlaylistPlacements(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		playlistID := chi.URLParam(r, "playlistId")

		var request setPlacementsRequest
		if _, err := decodeJSONBody(r, &request); err != nil {
			respondBadRequest(w, "invalid request body")
			return
		}
		if request.Placements == nil {
			respondBadRequest(w, "placements is required")
			return
		}

		ctx := r.Context()
		repo := ds.PlaylistFolder()
		folders, err := repo.GetAll(ctx)
		if err != nil {
			respondServerError(w, err)
			return
		}

		placements := make(model.PlaylistPlacements, 0, len(*request.Placements))
		for _, item := range *request.Placements {
			folderID := resolveFolderIDFromPath(item.FolderID)
			if folderID != model.PlaylistFolderRootID {
				if _, folderFound := findFolder(folders, folderID); !folderFound {
					respondNotFound(w)
					return
				}
			}
			placements = append(placements, model.PlaylistPlacement{
				PlaylistID: playlistID,
				FolderID:   folderID,
				SortOrder:  item.SortOrder,
			})
		}

		if err = repo.SetPlacements(ctx, playlistID, placements); err != nil {
			respondServerError(w, err)
			return
		}

		response := playlistPlacementsResponse{
			PlaylistID: playlistID,
			Placements: make([]playlistPlacementResponse, 0, len(placements)),
		}
		for _, placement := range placements {
			response.Placements = append(response.Placements, newPlaylistPlacementResponse(placement))
		}
		respondWithJSON(w, response)
	}
}

// folderExistsForUser guards placement writes: an unknown or foreign folder is a 404. The
// root folder always exists. It writes the error response itself and reports whether the
// caller should continue.
func folderExistsForUser(ctx context.Context, w http.ResponseWriter, repo model.PlaylistFolderRepository, folderID string) bool {
	if folderID == model.PlaylistFolderRootID {
		return true
	}
	if _, err := repo.Get(ctx, folderID); err != nil {
		if isNotFoundError(err) {
			respondNotFound(w)
			return false
		}
		respondServerError(w, err)
		return false
	}
	return true
}
