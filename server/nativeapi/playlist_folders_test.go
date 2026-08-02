package nativeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- In-memory mock for PlaylistFolderRepository ---

type mockPlaylistFolderRepo struct {
	mu          sync.Mutex
	folders     map[string]*model.PlaylistFolder
	memberships map[string]map[string]bool // folderID -> set of playlistIDs
	ownerID     string
	isAdmin     bool
	nextID      int
}

func newMockPlaylistFolderRepo(ownerID string, isAdmin bool) *mockPlaylistFolderRepo {
	return &mockPlaylistFolderRepo{
		folders:     make(map[string]*model.PlaylistFolder),
		memberships: make(map[string]map[string]bool),
		ownerID:     ownerID,
		isAdmin:     isAdmin,
	}
}

// --- rest.Repository methods ---

func (m *mockPlaylistFolderRepo) Count(_ ...rest.QueryOptions) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := int64(0)
	for _, folder := range m.folders {
		if m.isAdmin || folder.OwnerID == m.ownerID {
			count++
		}
	}
	return count, nil
}

func (m *mockPlaylistFolderRepo) Read(id string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, ok := m.folders[id]
	if !ok {
		return nil, rest.ErrNotFound
	}
	if !m.isAdmin && folder.OwnerID != m.ownerID {
		return nil, rest.ErrNotFound
	}
	return folder, nil
}

func (m *mockPlaylistFolderRepo) ReadAll(_ ...rest.QueryOptions) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result model.PlaylistFolders
	for _, folder := range m.folders {
		if m.isAdmin || folder.OwnerID == m.ownerID {
			result = append(result, *folder)
		}
	}
	if result == nil {
		result = model.PlaylistFolders{}
	}
	return result, nil
}

func (m *mockPlaylistFolderRepo) EntityName() string {
	return "playlist_folder"
}

func (m *mockPlaylistFolderRepo) NewInstance() interface{} {
	return &model.PlaylistFolder{}
}

// --- rest.Persistable methods ---

func (m *mockPlaylistFolderRepo) Save(entity interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder := entity.(*model.PlaylistFolder)
	m.nextID++
	folder.ID = fmt.Sprintf("folder-%d", m.nextID)
	folder.OwnerID = m.ownerID
	now := time.Now()
	folder.CreatedAt = now
	folder.UpdatedAt = now
	stored := *folder
	m.folders[folder.ID] = &stored
	return folder.ID, nil
}

func (m *mockPlaylistFolderRepo) Update(id string, entity interface{}, _ ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.folders[id]
	if !ok {
		return rest.ErrNotFound
	}
	if !m.isAdmin && existing.OwnerID != m.ownerID {
		return rest.ErrNotFound
	}
	updated := entity.(*model.PlaylistFolder)
	existing.Name = updated.Name
	if updated.ParentID != "" {
		existing.ParentID = updated.ParentID
	}
	existing.UpdatedAt = time.Now()
	return nil
}

func (m *mockPlaylistFolderRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, ok := m.folders[id]
	if !ok {
		return rest.ErrNotFound
	}
	if !m.isAdmin && folder.OwnerID != m.ownerID {
		return rest.ErrNotFound
	}
	delete(m.folders, id)
	delete(m.memberships, id)
	return nil
}

// --- model.PlaylistFolderRepository methods ---

func (m *mockPlaylistFolderRepo) Put(folder *model.PlaylistFolder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if folder.ID == "" {
		m.nextID++
		folder.ID = fmt.Sprintf("folder-%d", m.nextID)
	}
	now := time.Now()
	if folder.CreatedAt.IsZero() {
		folder.CreatedAt = now
	}
	folder.UpdatedAt = now
	stored := *folder
	m.folders[folder.ID] = &stored
	return nil
}

func (m *mockPlaylistFolderRepo) Get(id string) (*model.PlaylistFolder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, ok := m.folders[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	if !m.isAdmin && folder.OwnerID != m.ownerID {
		return nil, model.ErrNotFound
	}
	result := *folder
	return &result, nil
}

func (m *mockPlaylistFolderRepo) GetAll(_ ...model.QueryOptions) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result model.PlaylistFolders
	for _, folder := range m.folders {
		if m.isAdmin || folder.OwnerID == m.ownerID {
			result = append(result, *folder)
		}
	}
	return result, nil
}

func (m *mockPlaylistFolderRepo) GetRoots(_ ...model.QueryOptions) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result model.PlaylistFolders
	for _, folder := range m.folders {
		if folder.ParentID == "" && (m.isAdmin || folder.OwnerID == m.ownerID) {
			result = append(result, *folder)
		}
	}
	return result, nil
}

func (m *mockPlaylistFolderRepo) GetChildren(parentID string) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result model.PlaylistFolders
	for _, folder := range m.folders {
		if folder.ParentID == parentID && (m.isAdmin || folder.OwnerID == m.ownerID) {
			result = append(result, *folder)
		}
	}
	return result, nil
}

func (m *mockPlaylistFolderRepo) AddPlaylist(folderID, playlistID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.folders[folderID]; !ok {
		return model.ErrNotFound
	}
	if m.memberships[folderID] == nil {
		m.memberships[folderID] = make(map[string]bool)
	}
	m.memberships[folderID][playlistID] = true
	return nil
}

func (m *mockPlaylistFolderRepo) RemovePlaylist(folderID, playlistID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.folders[folderID]; !ok {
		return model.ErrNotFound
	}
	if m.memberships[folderID] != nil {
		delete(m.memberships[folderID], playlistID)
	}
	return nil
}

func (m *mockPlaylistFolderRepo) GetPlaylistFolders(playlistID string) (model.PlaylistFolders, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result model.PlaylistFolders
	for folderID, playlists := range m.memberships {
		if playlists[playlistID] {
			if folder, ok := m.folders[folderID]; ok {
				result = append(result, *folder)
			}
		}
	}
	return result, nil
}

func (m *mockPlaylistFolderRepo) GetPlaylistIDs(folderID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	if playlists, ok := m.memberships[folderID]; ok {
		for playlistID := range playlists {
			result = append(result, playlistID)
		}
	}
	return result, nil
}

// --- Tests ---

var _ = Describe("Playlist Folder Endpoints", func() {
	var (
		router         http.Handler
		responseWriter *httptest.ResponseRecorder
		mockFolderRepo *mockPlaylistFolderRepo
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.SessionTimeout = time.Minute

		userRepo := tests.CreateMockUserRepo()
		mockFolderRepo = newMockPlaylistFolderRepo("user-1", false)

		dataStore := &tests.MockDataStore{
			MockedUser:           userRepo,
			MockedProperty:       &tests.MockedPropertyRepo{},
			MockedPlaylistFolder: mockFolderRepo,
		}

		auth.Init(dataStore)

		testUser := model.User{
			ID:          "user-1",
			UserName:    "testuser",
			Name:        "Test User",
			IsAdmin:     false,
			NewPassword: "testpass",
		}
		err := userRepo.Put(&testUser)
		Expect(err).ToNot(HaveOccurred())

		nativeRouter := New(dataStore, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil)
		router = server.JWTVerifier(nativeRouter)
		responseWriter = httptest.NewRecorder()
	})

	createAuthenticatedRequest := func(method, path string, body ...string) *http.Request {
		var bodyReader *strings.Reader
		if len(body) > 0 {
			bodyReader = strings.NewReader(body[0])
		} else {
			bodyReader = strings.NewReader("")
		}
		request := httptest.NewRequest(method, path, bodyReader)
		if len(body) > 0 {
			request.Header.Set("Content-Type", "application/json")
		}
		testUser := model.User{ID: "user-1", UserName: "testuser"}
		token, err := auth.CreateToken(&testUser)
		Expect(err).ToNot(HaveOccurred())
		request.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		return request
	}

	createUnauthenticatedRequest := func(method, path string) *http.Request {
		return httptest.NewRequest(method, path, nil)
	}

	Describe("CRUD operations", func() {
		It("POST /playlist/folder creates a folder and returns id", func() {
			request := createAuthenticatedRequest("POST", "/playlist/folder", `{"name":"Rock"}`)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var response map[string]string
			err := json.Unmarshal(responseWriter.Body.Bytes(), &response)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(HaveKey("id"))
			Expect(response["id"]).To(HavePrefix("folder-"))
		})

		It("GET /playlist/folder lists folders", func() {
			// Seed a folder
			err := mockFolderRepo.Put(&model.PlaylistFolder{Name: "Jazz", OwnerID: "user-1"})
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("GET", "/playlist/folder")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var folders model.PlaylistFolders
			err = json.Unmarshal(responseWriter.Body.Bytes(), &folders)
			Expect(err).ToNot(HaveOccurred())
			Expect(folders).To(HaveLen(1))
			Expect(folders[0].Name).To(Equal("Jazz"))
		})

		It("GET /playlist/folder/{id} returns folder detail with children and playlistIds", func() {
			// Seed parent folder
			parentFolder := &model.PlaylistFolder{Name: "Music", OwnerID: "user-1"}
			err := mockFolderRepo.Put(parentFolder)
			Expect(err).ToNot(HaveOccurred())
			parentID := parentFolder.ID

			// Seed child folder
			childFolder := &model.PlaylistFolder{Name: "Rock", OwnerID: "user-1", ParentID: parentID}
			err = mockFolderRepo.Put(childFolder)
			Expect(err).ToNot(HaveOccurred())

			// Add a playlist membership
			err = mockFolderRepo.AddPlaylist(parentID, "pls-42")
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("GET", "/playlist/folder/"+parentID)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var detail playlistFolderDetailResponse
			err = json.Unmarshal(responseWriter.Body.Bytes(), &detail)
			Expect(err).ToNot(HaveOccurred())
			Expect(detail.Name).To(Equal("Music"))
			Expect(detail.Children).To(HaveLen(1))
			Expect(detail.Children[0].Name).To(Equal("Rock"))
			Expect(detail.PlaylistIDs).To(ConsistOf("pls-42"))
		})

		It("PUT /playlist/folder/{id} updates the folder name", func() {
			folder := &model.PlaylistFolder{Name: "Old Name", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			request := createAuthenticatedRequest("PUT", "/playlist/folder/"+folderID, `{"name":"New Name"}`)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			// Verify the folder was updated in the repo
			updated, err := mockFolderRepo.Get(folderID)
			Expect(err).ToNot(HaveOccurred())
			Expect(updated.Name).To(Equal("New Name"))
		})

		It("DELETE /playlist/folder/{id} deletes the folder", func() {
			folder := &model.PlaylistFolder{Name: "Doomed", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			request := createAuthenticatedRequest("DELETE", "/playlist/folder/"+folderID)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			// Verify it's gone
			_, err = mockFolderRepo.Get(folderID)
			Expect(err).To(Equal(model.ErrNotFound))
		})
	})

	Describe("Authentication", func() {
		It("returns 401 for unauthenticated GET /playlist/folder", func() {
			request := createUnauthenticatedRequest("GET", "/playlist/folder")
			router.ServeHTTP(responseWriter, request)
			Expect(responseWriter.Code).To(Equal(http.StatusUnauthorized))
		})

		It("returns 401 for unauthenticated POST /playlist/folder", func() {
			request := createUnauthenticatedRequest("POST", "/playlist/folder")
			router.ServeHTTP(responseWriter, request)
			Expect(responseWriter.Code).To(Equal(http.StatusUnauthorized))
		})

		It("returns 401 for unauthenticated GET /playlist/folder/{id}", func() {
			request := createUnauthenticatedRequest("GET", "/playlist/folder/some-id")
			router.ServeHTTP(responseWriter, request)
			Expect(responseWriter.Code).To(Equal(http.StatusUnauthorized))
		})

		It("returns 401 for unauthenticated DELETE /playlist/folder/{id}", func() {
			request := createUnauthenticatedRequest("DELETE", "/playlist/folder/some-id")
			router.ServeHTTP(responseWriter, request)
			Expect(responseWriter.Code).To(Equal(http.StatusUnauthorized))
		})

		It("returns 401 for unauthenticated membership POST", func() {
			request := createUnauthenticatedRequest("POST", "/playlist/folder/some-id/playlist/pls-1")
			router.ServeHTTP(responseWriter, request)
			Expect(responseWriter.Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("Membership endpoints", func() {
		It("POST /playlist/folder/{id}/playlist/{playlistId} adds membership", func() {
			folder := &model.PlaylistFolder{Name: "Favorites", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			request := createAuthenticatedRequest("POST", "/playlist/folder/"+folderID+"/playlist/pls-1")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var response map[string]string
			err = json.Unmarshal(responseWriter.Body.Bytes(), &response)
			Expect(err).ToNot(HaveOccurred())
			Expect(response["status"]).To(Equal("ok"))

			// Verify membership
			playlistIDs, err := mockFolderRepo.GetPlaylistIDs(folderID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(ConsistOf("pls-1"))
		})

		It("DELETE /playlist/folder/{id}/playlist/{playlistId} removes membership", func() {
			folder := &model.PlaylistFolder{Name: "Favorites", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			// Add then remove
			err = mockFolderRepo.AddPlaylist(folderID, "pls-1")
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("DELETE", "/playlist/folder/"+folderID+"/playlist/pls-1")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			// Verify membership removed
			playlistIDs, err := mockFolderRepo.GetPlaylistIDs(folderID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(BeEmpty())
		})

		It("POST /playlist/folder/nonexistent/playlist/pls-1 returns 404", func() {
			request := createAuthenticatedRequest("POST", "/playlist/folder/nonexistent/playlist/pls-1")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Owner isolation", func() {
		It("user-2 cannot see folders owned by user-1", func() {
			// Create folder as user-1 (the repo's owner)
			folder := &model.PlaylistFolder{Name: "Secret", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			// Switch repo to user-2 perspective
			mockFolderRepo.mu.Lock()
			mockFolderRepo.ownerID = "user-2"
			mockFolderRepo.mu.Unlock()

			request := createAuthenticatedRequest("GET", "/playlist/folder/"+folderID)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusNotFound))
		})

		It("admin can see all folders regardless of owner", func() {
			// Create folder as user-1
			folder := &model.PlaylistFolder{Name: "Private", OwnerID: "user-1"}
			err := mockFolderRepo.Put(folder)
			Expect(err).ToNot(HaveOccurred())
			folderID := folder.ID

			// Switch repo to admin perspective with different ownerID
			mockFolderRepo.mu.Lock()
			mockFolderRepo.ownerID = "admin-user"
			mockFolderRepo.isAdmin = true
			mockFolderRepo.mu.Unlock()

			request := createAuthenticatedRequest("GET", "/playlist/folder/"+folderID)
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var detail playlistFolderDetailResponse
			err = json.Unmarshal(responseWriter.Body.Bytes(), &detail)
			Expect(err).ToNot(HaveOccurred())
			Expect(detail.Name).To(Equal("Private"))
		})
	})

	Describe("Edge cases", func() {
		It("GET /playlist/folder/nonexistent returns 404", func() {
			request := createAuthenticatedRequest("GET", "/playlist/folder/nonexistent")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusNotFound))
		})

		It("DELETE /playlist/folder/nonexistent returns 404", func() {
			request := createAuthenticatedRequest("DELETE", "/playlist/folder/nonexistent")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusNotFound))
		})
	})
})
