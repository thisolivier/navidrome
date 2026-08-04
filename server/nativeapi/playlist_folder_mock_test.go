package nativeapi

import (
	"fmt"
	"sync"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
)

// mockPlaylistFolderRepo is an in-memory stand-in for the persistence repository. It
// reproduces the two scoping rules the handlers rely on: folders are visible only to their
// owner, and placements only to the user that made them.
type mockPlaylistFolderRepo struct {
	mu             sync.Mutex
	folders        map[string]*model.PlaylistFolder
	placements     []model.PlaylistPlacement
	currentUserID  string
	nextFolderSeed int
}

func newMockPlaylistFolderRepo(currentUserID string) *mockPlaylistFolderRepo {
	return &mockPlaylistFolderRepo{
		folders:       make(map[string]*model.PlaylistFolder),
		currentUserID: currentUserID,
	}
}

func (m *mockPlaylistFolderRepo) switchUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentUserID = userID
}

// visibleFolder must be called with the mutex held.
func (m *mockPlaylistFolderRepo) visibleFolder(id string) (*model.PlaylistFolder, bool) {
	folder, found := m.folders[id]
	if !found || folder.OwnerID != m.currentUserID {
		return nil, false
	}
	return folder, true
}

// --- model.PlaylistFolderRepository: folders ---

func (m *mockPlaylistFolderRepo) Put(folder *model.PlaylistFolder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if folder.OwnerID == "" {
		folder.OwnerID = m.currentUserID
	}
	if folder.ID == "" {
		m.nextFolderSeed++
		folder.ID = fmt.Sprintf("folder-%d", m.nextFolderSeed)
		folder.CreatedAt = time.Now()
	}
	folder.UpdatedAt = time.Now()
	stored := *folder
	m.folders[folder.ID] = &stored
	return nil
}

func (m *mockPlaylistFolderRepo) Get(id string) (*model.PlaylistFolder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, visible := m.visibleFolder(id)
	if !visible {
		return nil, model.ErrNotFound
	}
	result := *folder
	return &result, nil
}

func (m *mockPlaylistFolderRepo) GetAll(_ ...model.QueryOptions) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folders := model.PlaylistFolders{}
	for _, folder := range m.folders {
		if folder.OwnerID == m.currentUserID {
			folders = append(folders, *folder)
		}
	}
	return folders, nil
}

func (m *mockPlaylistFolderRepo) GetChildren(parentID string) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	children := model.PlaylistFolders{}
	for _, folder := range m.folders {
		if folder.ParentID == parentID && folder.OwnerID == m.currentUserID {
			children = append(children, *folder)
		}
	}
	return children, nil
}

func (m *mockPlaylistFolderRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, visible := m.visibleFolder(id)
	if !visible {
		return model.ErrNotFound
	}
	for _, candidate := range m.folders {
		if candidate.ParentID == id && candidate.OwnerID == m.currentUserID {
			candidate.ParentID = folder.ParentID
		}
	}
	m.placements = promotePlacementsOf(m.placements, id, folder.ParentID)
	delete(m.folders, id)
	return nil
}

// promotePlacementsOf re-homes every placement of a folder into that folder's parent,
// keeping its sort order. A placement that already exists in the parent wins untouched.
func promotePlacementsOf(placements []model.PlaylistPlacement, folderID, parentID string) []model.PlaylistPlacement {
	placementKey := func(placement model.PlaylistPlacement) string {
		return placement.UserID + "\x00" + placement.PlaylistID + "\x00" + placement.FolderID
	}

	survivors := make([]model.PlaylistPlacement, 0, len(placements))
	occupiedKeys := map[string]bool{}
	for _, placement := range placements {
		if placement.FolderID != folderID {
			survivors = append(survivors, placement)
			occupiedKeys[placementKey(placement)] = true
		}
	}
	for _, placement := range placements {
		if placement.FolderID != folderID {
			continue
		}
		promoted := placement
		promoted.FolderID = parentID
		if occupiedKeys[placementKey(promoted)] {
			continue
		}
		survivors = append(survivors, promoted)
		occupiedKeys[placementKey(promoted)] = true
	}
	return survivors
}

// --- model.PlaylistFolderRepository: placements ---

func (m *mockPlaylistFolderRepo) GetPlacements() (model.PlaylistPlacements, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	placements := model.PlaylistPlacements{}
	for _, placement := range m.placements {
		if placement.UserID == m.currentUserID {
			placements = append(placements, placement)
		}
	}
	return placements, nil
}

func (m *mockPlaylistFolderRepo) PutPlacement(placement model.PlaylistPlacement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	placement.UserID = m.currentUserID
	for index, existing := range m.placements {
		if existing.UserID == placement.UserID &&
			existing.PlaylistID == placement.PlaylistID &&
			existing.FolderID == placement.FolderID {
			m.placements[index] = placement
			return nil
		}
	}
	m.placements = append(m.placements, placement)
	return nil
}

func (m *mockPlaylistFolderRepo) DeletePlacement(playlistID, folderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining := m.placements[:0]
	for _, placement := range m.placements {
		matches := placement.UserID == m.currentUserID &&
			placement.PlaylistID == playlistID &&
			placement.FolderID == folderID
		if !matches {
			remaining = append(remaining, placement)
		}
	}
	m.placements = remaining
	return nil
}

func (m *mockPlaylistFolderRepo) SetPlacements(playlistID string, placements model.PlaylistPlacements) error {
	m.mu.Lock()
	remaining := m.placements[:0]
	for _, placement := range m.placements {
		if placement.UserID != m.currentUserID || placement.PlaylistID != playlistID {
			remaining = append(remaining, placement)
		}
	}
	m.placements = remaining
	m.mu.Unlock()

	for _, placement := range placements {
		placement.PlaylistID = playlistID
		if err := m.PutPlacement(placement); err != nil {
			return err
		}
	}
	return nil
}

// --- model.ResourceRepository (unused by the routes, required by the interface) ---

func (m *mockPlaylistFolderRepo) Count(_ ...rest.QueryOptions) (int64, error) {
	folders, _ := m.GetAll()
	return int64(len(folders)), nil
}

func (m *mockPlaylistFolderRepo) Read(id string) (any, error) {
	return m.Get(id)
}

func (m *mockPlaylistFolderRepo) ReadAll(_ ...rest.QueryOptions) (any, error) {
	return m.GetAll()
}

func (m *mockPlaylistFolderRepo) EntityName() string { return "playlist_folder" }

func (m *mockPlaylistFolderRepo) NewInstance() any { return &model.PlaylistFolder{} }

var _ model.PlaylistFolderRepository = (*mockPlaylistFolderRepo)(nil)
