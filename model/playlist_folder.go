package model

import "time"

type PlaylistFolder struct {
	ID        string    `structs:"id" json:"id"`
	Name      string    `structs:"name" json:"name"`
	ParentID  string    `structs:"parent_id" json:"parentId"`
	OwnerID   string    `structs:"owner_id" json:"ownerId"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`
}

type PlaylistFolders []PlaylistFolder

type PlaylistFolderRepository interface {
	ResourceRepository
	Put(folder *PlaylistFolder) error
	Get(id string) (*PlaylistFolder, error)
	GetAll(options ...QueryOptions) (PlaylistFolders, error)
	GetRoots(options ...QueryOptions) (PlaylistFolders, error)
	GetChildren(parentID string) (PlaylistFolders, error)
	Delete(id string) error
	AddPlaylist(folderID, playlistID string) error
	RemovePlaylist(folderID, playlistID string) error
	GetPlaylistFolders(playlistID string) (PlaylistFolders, error)
	GetPlaylistIDs(folderID string) ([]string, error)
}
