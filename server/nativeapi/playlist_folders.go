package nativeapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
)

// addPlaylistFolderRoute registers the playlist organization endpoints:
//
//	GET    /playlist/folder                              the whole tree (folders + placements)
//	POST   /playlist/folder                              create a folder
//	PUT    /playlist/folder/{id}                         rename / re-parent / re-order a folder
//	DELETE /playlist/folder/{id}                         delete a folder, releasing its contents
//	PUT    /playlist/folder/{id}/playlist/{playlistId}   create or re-order one placement
//	DELETE /playlist/folder/{id}/playlist/{playlistId}   remove one placement (idempotent)
//	PUT    /playlist/{playlistId}/placements             replace every placement of a playlist
//
// {id} accepts the reserved value "root" to address the root of the tree.
// This must be registered before addPlaylistRoute, which mounts a subrouter on /playlist.
func (api *Router) addPlaylistFolderRoute(r chi.Router) {
	r.Route("/playlist/folder", func(r chi.Router) {
		r.Get("/", getPlaylistFolderTree(api.ds))
		r.Post("/", createPlaylistFolder(api.ds))

		r.Route("/{id}", func(r chi.Router) {
			r.Put("/", updatePlaylistFolder(api.ds))
			r.Delete("/", deletePlaylistFolder(api.ds))

			r.Put("/playlist/{playlistId}", putPlaylistPlacement(api.ds))
			r.Delete("/playlist/{playlistId}", deletePlaylistPlacement(api.ds))
		})
	})

	r.Put("/playlist/{playlistId}/placements", setPlaylistPlacements(api.ds))
}

// getPlaylistFolderTree returns the caller's entire organization in one envelope, using
// exactly two queries regardless of library size.
func getPlaylistFolderTree(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := ds.PlaylistFolder(r.Context())

		folders, err := repo.GetAll()
		if err != nil {
			respondServerError(w, err)
			return
		}
		placements, err := repo.GetPlacements()
		if err != nil {
			respondServerError(w, err)
			return
		}

		response := playlistFolderTreeResponse{
			FolderAPIVersion: playlistFolderAPIVersion,
			Folders:          make([]playlistFolderResponse, 0, len(folders)),
			Placements:       make([]playlistPlacementResponse, 0, len(placements)),
		}
		for _, folder := range folders {
			response.Folders = append(response.Folders, newPlaylistFolderResponse(folder))
		}
		for _, placement := range placements {
			response.Placements = append(response.Placements, newPlaylistPlacementResponse(placement))
		}
		respondWithJSON(w, response)
	}
}

func createPlaylistFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request playlistFolderWriteRequest
		if _, err := decodeJSONBody(r, &request); err != nil {
			respondBadRequest(w, "invalid request body")
			return
		}
		if request.Name == nil || *request.Name == "" {
			respondBadRequest(w, "name is required")
			return
		}

		repo := ds.PlaylistFolder(r.Context())
		folder := model.PlaylistFolder{Name: *request.Name, SortOrder: request.SortOrder}
		if request.ParentID != nil && *request.ParentID != model.PlaylistFolderRootID {
			if _, err := repo.Get(*request.ParentID); err != nil {
				if isNotFoundError(err) {
					respondNotFound(w)
					return
				}
				respondServerError(w, err)
				return
			}
			folder.ParentID = *request.ParentID
		}

		if err := repo.Put(&folder); err != nil {
			respondServerError(w, err)
			return
		}
		respondWithJSON(w, newPlaylistFolderResponse(folder))
	}
}

// updatePlaylistFolder applies a partial update. Re-parenting a folder under itself or one
// of its own descendants is rejected with 400.
func updatePlaylistFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := chi.URLParam(r, "id")

		var request playlistFolderWriteRequest
		rawFields, err := decodeJSONBody(r, &request)
		if err != nil {
			respondBadRequest(w, "invalid request body")
			return
		}

		repo := ds.PlaylistFolder(r.Context())
		folders, err := repo.GetAll()
		if err != nil {
			respondServerError(w, err)
			return
		}
		folder, found := findFolder(folders, folderID)
		if !found {
			respondNotFound(w)
			return
		}

		if request.Name != nil {
			if *request.Name == "" {
				respondBadRequest(w, "name cannot be empty")
				return
			}
			folder.Name = *request.Name
		}
		if request.ParentID != nil {
			proposedParentID := *request.ParentID
			if proposedParentID != model.PlaylistFolderRootID {
				if _, parentExists := findFolder(folders, proposedParentID); !parentExists {
					respondNotFound(w)
					return
				}
			}
			if wouldCreateCycle(folders, folderID, proposedParentID) {
				respondBadRequest(w, "a folder cannot be moved inside itself or one of its descendants")
				return
			}
			folder.ParentID = proposedParentID
		}
		if _, sortOrderSupplied := rawFields["sortOrder"]; sortOrderSupplied {
			folder.SortOrder = request.SortOrder
		}

		if err = repo.Put(&folder); err != nil {
			respondServerError(w, err)
			return
		}
		respondWithJSON(w, newPlaylistFolderResponse(folder))
	}
}

// deletePlaylistFolder removes the folder, re-parents its children to the deleted folder's
// parent and releases its placements. Playlists themselves are never touched.
func deletePlaylistFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := chi.URLParam(r, "id")

		err := ds.PlaylistFolder(r.Context()).Delete(folderID)
		if err != nil {
			if isNotFoundError(err) {
				respondNotFound(w)
				return
			}
			respondServerError(w, err)
			return
		}
		respondWithJSON(w, map[string]string{"id": folderID})
	}
}
