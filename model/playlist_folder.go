package model

import "time"

// PlaylistFolderRootID is the reserved folder ID for the root of a user's tree.
// A placement with this folder ID is an explicit root placement, which is what carries
// root ordering. A playlist with no placement at all is implicitly at the root, unordered.
const PlaylistFolderRootID = ""

// PlaylistFolder is a node in one user's private organization tree. Folders never own
// playlists: they are an organizational schema laid over them.
type PlaylistFolder struct {
	ID       string `structs:"id" json:"id"`
	Name     string `structs:"name" json:"name"`
	ParentID string `structs:"parent_id" json:"parentId"`
	OwnerID  string `structs:"owner_id" json:"ownerId"`
	// SortOrder is client-assigned, stored unnormalized, and may be duplicated. Sibling
	// folders and placements share one ordering space per parent. nil means unordered.
	SortOrder *int      `structs:"sort_order" json:"sortOrder,omitempty"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`
}

type PlaylistFolders []PlaylistFolder

// PlaylistPlacement records that a playlist appears in a folder for one user. There is at
// most one placement per (user, playlist, folder), so the same playlist can be filed in
// several folders and ordered independently in each.
type PlaylistPlacement struct {
	UserID     string `structs:"user_id" json:"-"`
	PlaylistID string `structs:"playlist_id" json:"playlistId"`
	FolderID   string `structs:"folder_id" json:"folderId"`
	SortOrder  *int   `structs:"sort_order" json:"sortOrder,omitempty"`
}

type PlaylistPlacements []PlaylistPlacement

// PlaylistFolderRepository owns both the folder tree and the placements. Every method is
// scoped to the user in the request context: folders by owner, placements by user.
type PlaylistFolderRepository interface {
	ResourceRepository

	Put(folder *PlaylistFolder) error
	Get(id string) (*PlaylistFolder, error)
	GetAll(options ...QueryOptions) (PlaylistFolders, error)
	GetChildren(parentID string) (PlaylistFolders, error)
	// Delete removes the folder, re-parents its children to the folder's own parent and
	// releases the placements it held. Playlists are never touched.
	Delete(id string) error

	GetPlacements() (PlaylistPlacements, error)
	// PutPlacement creates the placement or updates the sort order of an existing one.
	PutPlacement(placement PlaylistPlacement) error
	// DeletePlacement is idempotent: removing a placement that does not exist is not an error.
	DeletePlacement(playlistID, folderID string) error
	// SetPlacements replaces every placement of a playlist for the current user. An empty
	// list unfiles the playlist back to the implicit root.
	SetPlacements(playlistID string, placements PlaylistPlacements) error
}
