package nativeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// --- Mock Maintenance service ---

type mockMaintenance struct {
	deletedIDs []string
	err        error
}

func (m *mockMaintenance) DeleteMissingFiles(_ context.Context, ids []string) error {
	m.deletedIDs = append(m.deletedIDs, ids...)
	return m.err
}

func (m *mockMaintenance) DeleteAllMissingFiles(_ context.Context) error {
	return m.err
}

// --- Tests ---

var _ = Describe("Song Delete Endpoint", func() {
	var (
		router         http.Handler
		responseWriter *httptest.ResponseRecorder
		mediaFileRepo  *tests.MockMediaFileRepo
		maintenance    *mockMaintenance
		tempMusicDir   string
		tempTrashDir   string
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.SessionTimeout = time.Minute

		// Create temp directories for music files and trash
		var err error
		tempMusicDir, err = os.MkdirTemp("", "song-delete-test-music-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(tempMusicDir) })

		tempTrashDir, err = os.MkdirTemp("", "song-delete-test-trash-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(tempTrashDir) })

		conf.Server.MusicFolder = tempMusicDir
		conf.Server.TrashDirectory = tempTrashDir

		// Setup mock repos
		userRepo := tests.CreateMockUserRepo()
		mediaFileRepo = tests.CreateMockMediaFileRepo()
		maintenance = &mockMaintenance{}

		dataStore := &tests.MockDataStore{
			MockedUser:      userRepo,
			MockedProperty:  &tests.MockedPropertyRepo{},
			MockedMediaFile: mediaFileRepo,
		}

		auth.Init(dataStore)

		testUser := model.User{
			ID:          "user-1",
			UserName:    "testuser",
			Name:        "Test User",
			IsAdmin:     false,
			NewPassword: "testpass",
		}
		err = userRepo.Put(&testUser)
		Expect(err).ToNot(HaveOccurred())

		nativeRouter := New(dataStore, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), maintenance, nil, nil)
		router = server.JWTVerifier(nativeRouter)
		responseWriter = httptest.NewRecorder()
	})

	createAuthenticatedRequest := func(method, path string) *http.Request {
		request := httptest.NewRequest(method, path, strings.NewReader(""))
		testUser := model.User{ID: "user-1", UserName: "testuser"}
		token, err := auth.CreateToken(&testUser)
		Expect(err).ToNot(HaveOccurred())
		request.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		return request
	}

	// Helper to seed a media file with a real temp file on disk
	seedMediaFileWithDiskFile := func(songID string) string {
		songFilePath := filepath.Join("music", "artist", "album", "song.mp3")
		absPath := filepath.Join(tempMusicDir, songFilePath)

		// Create parent directories and file
		err := os.MkdirAll(filepath.Dir(absPath), 0755)
		Expect(err).ToNot(HaveOccurred())
		err = os.WriteFile(absPath, []byte("fake mp3 content"), 0644)
		Expect(err).ToNot(HaveOccurred())

		mediaFile := model.MediaFile{
			ID:          songID,
			Title:       "Test Song",
			Path:        songFilePath,
			LibraryPath: tempMusicDir,
			LibraryID:   1,
		}
		mediaFileRepo.Data[songID] = &mediaFile

		return absPath
	}

	Describe("DELETE /song/{id}", func() {
		It("returns 200 and deletes an existing song", func() {
			absPath := seedMediaFileWithDiskFile("song-1")

			// Verify file exists before delete
			_, err := os.Stat(absPath)
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("DELETE", "/song/song-1")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var response songDeleteResponse
			err = json.Unmarshal(responseWriter.Body.Bytes(), &response)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.Status).To(Equal("deleted"))
			Expect(response.ID).To(Equal("song-1"))
			Expect(response.TrashedPath).To(ContainSubstring("song.mp3"))

			// Verify maintenance was called
			Expect(maintenance.deletedIDs).To(ConsistOf("song-1"))
		})

		It("returns 404 for unknown song ID", func() {
			request := createAuthenticatedRequest("DELETE", "/song/nonexistent-id")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 200 when file is already missing from disk", func() {
			// Seed a media file whose path does not exist on disk
			mediaFile := model.MediaFile{
				ID:          "song-missing",
				Title:       "Ghost Song",
				Path:        "does/not/exist/song.mp3",
				LibraryPath: tempMusicDir,
				LibraryID:   1,
			}
			mediaFileRepo.Data["song-missing"] = &mediaFile

			request := createAuthenticatedRequest("DELETE", "/song/song-missing")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			var response songDeleteResponse
			err := json.Unmarshal(responseWriter.Body.Bytes(), &response)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.Status).To(Equal("deleted"))
			Expect(response.TrashedPath).To(Equal("(file was already missing)"))

			// Verify maintenance still purges the DB record
			Expect(maintenance.deletedIDs).To(ConsistOf("song-missing"))
		})

		It("creates trash directory automatically when it does not exist", func() {
			// Set trash dir to a non-existent sub-path
			nonExistentTrashDir := filepath.Join(tempTrashDir, "auto-created-subdir")
			conf.Server.TrashDirectory = nonExistentTrashDir

			// Verify it doesn't exist yet
			_, err := os.Stat(nonExistentTrashDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			absPath := seedMediaFileWithDiskFile("song-auto-trash")

			// Verify source file exists
			_, err = os.Stat(absPath)
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("DELETE", "/song/song-auto-trash")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			// Verify the trash directory was created
			stat, err := os.Stat(nonExistentTrashDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(stat.IsDir()).To(BeTrue())
		})

		It("removes the original file from disk after delete", func() {
			absPath := seedMediaFileWithDiskFile("song-verify-gone")

			// Verify file exists before
			_, err := os.Stat(absPath)
			Expect(err).ToNot(HaveOccurred())

			request := createAuthenticatedRequest("DELETE", "/song/song-verify-gone")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusOK))

			// Verify original file is gone
			_, err = os.Stat(absPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("returns 500 when trash directory is inside library path", func() {
			// Configure trash dir inside the music library path
			conf.Server.TrashDirectory = filepath.Join(tempMusicDir, ".trash")

			seedMediaFileWithDiskFile("song-bad-trash")

			request := createAuthenticatedRequest("DELETE", "/song/song-bad-trash")
			router.ServeHTTP(responseWriter, request)

			Expect(responseWriter.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
