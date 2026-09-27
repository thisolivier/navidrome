package nativeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testUserID = "user-1"

// mockCtx feeds the ctx-first repository interface when tests drive the in-memory mock
// directly; the mock scopes by its own currentUserID and ignores the context.
var mockCtx = context.Background()

// playlistFolderTestEnv wires the native API router to an in-memory folder repository.
type playlistFolderTestEnv struct {
	router         http.Handler
	folderRepo     *mockPlaylistFolderRepo
	responseWriter *httptest.ResponseRecorder
}

func setupPlaylistFolderTestEnv() *playlistFolderTestEnv {
	DeferCleanup(configtest.SetupConfig())
	conf.Server.SessionTimeout = time.Minute
	conf.Server.EnableSharing = false // the env wires no share service

	userRepo := tests.CreateMockUserRepo()
	folderRepo := newMockPlaylistFolderRepo(testUserID)

	dataStore := &tests.MockDataStore{
		MockedUser:           userRepo,
		MockedProperty:       &tests.MockedPropertyRepo{},
		MockedPlaylistFolder: folderRepo,
	}
	auth.Init(dataStore)

	testUser := model.User{
		ID:          testUserID,
		UserName:    "testuser",
		Name:        "Test User",
		IsAdmin:     false,
		NewPassword: "testpass",
	}
	Expect(userRepo.Put(mockCtx, &testUser)).To(Succeed())

	nativeRouter := New(dataStore, nil, playlists.NewPlaylists(dataStore, nil), nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil)
	return &playlistFolderTestEnv{
		router:         server.JWTVerifier(nativeRouter),
		folderRepo:     folderRepo,
		responseWriter: httptest.NewRecorder(),
	}
}

// call issues an authenticated request and records the response.
func (env *playlistFolderTestEnv) call(method, path string, body ...string) *httptest.ResponseRecorder {
	env.responseWriter = httptest.NewRecorder()
	requestBody := ""
	if len(body) > 0 {
		requestBody = body[0]
	}
	request := httptest.NewRequest(method, path, strings.NewReader(requestBody))
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	token, err := auth.CreateToken(&model.User{ID: testUserID, UserName: "testuser"})
	Expect(err).ToNot(HaveOccurred())
	request.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)

	env.router.ServeHTTP(env.responseWriter, request)
	return env.responseWriter
}

// callWithNilBody issues an authenticated request carrying no request body at all, which is
// what the iOS client sends when it has no sortOrder to supply.
func (env *playlistFolderTestEnv) callWithNilBody(method, path string) *httptest.ResponseRecorder {
	env.responseWriter = httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	token, err := auth.CreateToken(&model.User{ID: testUserID, UserName: "testuser"})
	Expect(err).ToNot(HaveOccurred())
	request.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)

	env.router.ServeHTTP(env.responseWriter, request)
	return env.responseWriter
}

func (env *playlistFolderTestEnv) callUnauthenticated(method, path string) *httptest.ResponseRecorder {
	env.responseWriter = httptest.NewRecorder()
	env.router.ServeHTTP(env.responseWriter, httptest.NewRequest(method, path, nil))
	return env.responseWriter
}

// seedFolder inserts a folder owned by the test user directly in the repository.
func (env *playlistFolderTestEnv) seedFolder(name, parentID string) *model.PlaylistFolder {
	folder := &model.PlaylistFolder{Name: name, ParentID: parentID, OwnerID: testUserID}
	Expect(env.folderRepo.Put(mockCtx, folder)).To(Succeed())
	return folder
}

func sortOrderPointer(value int) *int {
	return &value
}
