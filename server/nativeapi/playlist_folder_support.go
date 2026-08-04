package nativeapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
)

// playlistFolderAPIVersion is carried in the folder tree envelope so clients can detect
// which placement model the server implements. Version 2 is the placement-relation model.
const playlistFolderAPIVersion = 2

// rootFolderPathSegment addresses the root of a user's tree in a path where a folder ID is
// expected. It maps to model.PlaylistFolderRootID (the empty folder ID).
const rootFolderPathSegment = "root"

// --- Response shapes (contracted with the iOS client; field names are fixed) ---

type playlistFolderResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId"`
	SortOrder *int      `json:"sortOrder,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type playlistPlacementResponse struct {
	PlaylistID string `json:"playlistId"`
	FolderID   string `json:"folderId"`
	SortOrder  *int   `json:"sortOrder,omitempty"`
}

type playlistFolderTreeResponse struct {
	FolderAPIVersion int                         `json:"folderApiVersion"`
	Folders          []playlistFolderResponse    `json:"folders"`
	Placements       []playlistPlacementResponse `json:"placements"`
}

type playlistPlacementsResponse struct {
	PlaylistID string                      `json:"playlistId"`
	Placements []playlistPlacementResponse `json:"placements"`
}

func newPlaylistFolderResponse(folder model.PlaylistFolder) playlistFolderResponse {
	return playlistFolderResponse{
		ID:        folder.ID,
		Name:      folder.Name,
		ParentID:  folder.ParentID,
		SortOrder: folder.SortOrder,
		CreatedAt: folder.CreatedAt,
		UpdatedAt: folder.UpdatedAt,
	}
}

func newPlaylistPlacementResponse(placement model.PlaylistPlacement) playlistPlacementResponse {
	return playlistPlacementResponse{
		PlaylistID: placement.PlaylistID,
		FolderID:   placement.FolderID,
		SortOrder:  placement.SortOrder,
	}
}

// --- Request bodies ---

// playlistFolderWriteRequest is used by both create and update. The update handler also
// inspects the raw field set, so it can tell "sortOrder omitted" from "sortOrder: null".
type playlistFolderWriteRequest struct {
	Name      *string `json:"name"`
	ParentID  *string `json:"parentId"`
	SortOrder *int    `json:"sortOrder"`
}

type placementWriteRequest struct {
	SortOrder *int `json:"sortOrder"`
}

type setPlacementsRequest struct {
	Placements *[]placementRequestItem `json:"placements"`
}

type placementRequestItem struct {
	FolderID  string `json:"folderId"`
	SortOrder *int   `json:"sortOrder"`
}

// decodeJSONBody decodes an optional JSON object body. An empty body yields the zero value
// and an empty raw field set, which callers read as "no fields supplied".
func decodeJSONBody(r *http.Request, target any) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	rawFields := map[string]json.RawMessage{}
	if err = json.Unmarshal(body, &rawFields); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(body, target); err != nil {
		return nil, err
	}
	return rawFields, nil
}

// --- Errors and responses ---

// isNotFoundError covers both not-found sentinels: the REST layer matches rest.ErrNotFound
// by identity, while repositories return model.ErrNotFound.
func isNotFoundError(err error) bool {
	return errors.Is(err, model.ErrNotFound) || errors.Is(err, rest.ErrNotFound)
}

func respondWithJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// respondNotFound is deliberately vague: unknown and foreign folder IDs are indistinguishable
// so that other users' trees cannot be probed.
func respondNotFound(w http.ResponseWriter) {
	http.Error(w, "not found", http.StatusNotFound)
}

func respondBadRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}

func respondServerError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// --- Tree helpers ---

// resolveFolderIDFromPath maps the reserved "root" segment onto the empty folder ID.
func resolveFolderIDFromPath(pathValue string) string {
	if pathValue == rootFolderPathSegment {
		return model.PlaylistFolderRootID
	}
	return pathValue
}

func findFolder(folders model.PlaylistFolders, folderID string) (model.PlaylistFolder, bool) {
	for _, folder := range folders {
		if folder.ID == folderID {
			return folder, true
		}
	}
	return model.PlaylistFolder{}, false
}

// wouldCreateCycle reports whether re-parenting folderID under proposedParentID would make
// the folder its own ancestor. It walks the parent chain upwards from the proposed parent.
func wouldCreateCycle(folders model.PlaylistFolders, folderID, proposedParentID string) bool {
	if proposedParentID == model.PlaylistFolderRootID {
		return false
	}
	parentByFolderID := make(map[string]string, len(folders))
	for _, folder := range folders {
		parentByFolderID[folder.ID] = folder.ParentID
	}
	visitedFolderIDs := make(map[string]bool, len(folders))
	for cursor := proposedParentID; cursor != model.PlaylistFolderRootID; {
		if cursor == folderID {
			return true
		}
		if visitedFolderIDs[cursor] {
			return false // pre-existing cycle in stored data; stop walking
		}
		visitedFolderIDs[cursor] = true
		parentID, found := parentByFolderID[cursor]
		if !found {
			return false
		}
		cursor = parentID
	}
	return false
}
