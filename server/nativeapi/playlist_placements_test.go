package nativeapi

import (
	"encoding/json"
	"net/http"

	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Playlist Placement Endpoints", func() {
	var env *playlistFolderTestEnv

	BeforeEach(func() {
		env = setupPlaylistFolderTestEnv()
	})

	placementsForPlaylist := func(playlistID string) model.PlaylistPlacements {
		allPlacements, err := env.folderRepo.GetPlacements(mockCtx)
		Expect(err).ToNot(HaveOccurred())
		matching := model.PlaylistPlacements{}
		for _, placement := range allPlacements {
			if placement.PlaylistID == playlistID {
				matching = append(matching, placement)
			}
		}
		return matching
	}

	Describe("PUT /playlist/folder/{id}/playlist/{playlistId}", func() {
		It("creates a placement with a sort order", func() {
			folder := env.seedFolder("Favorites", "")

			recorder := env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1", `{"sortOrder":3}`)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var placement playlistPlacementResponse
			Expect(json.Unmarshal(recorder.Body.Bytes(), &placement)).To(Succeed())
			Expect(placement.PlaylistID).To(Equal("pls-1"))
			Expect(placement.FolderID).To(Equal(folder.ID))
			Expect(*placement.SortOrder).To(Equal(3))

			stored := placementsForPlaylist("pls-1")
			Expect(stored).To(HaveLen(1))
			Expect(*stored[0].SortOrder).To(Equal(3))
		})

		It("succeeds when the request carries no body at all", func() {
			folder := env.seedFolder("Favorites", "")

			recorder := env.callWithNilBody("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1")
			Expect(recorder.Code).To(Equal(http.StatusOK))

			stored := placementsForPlaylist("pls-1")
			Expect(stored).To(HaveLen(1))
			Expect(stored[0].SortOrder).To(BeNil())
		})

		It("succeeds with an empty body or an empty JSON object", func() {
			folder := env.seedFolder("Favorites", "")

			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-2", `{}`).Code).To(Equal(http.StatusOK))

			Expect(placementsForPlaylist("pls-1")).To(HaveLen(1))
			Expect(placementsForPlaylist("pls-1")[0].SortOrder).To(BeNil())
			Expect(placementsForPlaylist("pls-2")).To(HaveLen(1))
			Expect(placementsForPlaylist("pls-2")[0].SortOrder).To(BeNil())
		})

		It("updates the sort order of an existing placement", func() {
			folder := env.seedFolder("Favorites", "")

			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1", `{"sortOrder":1}`).Code).To(Equal(http.StatusOK))
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1", `{"sortOrder":8}`).Code).To(Equal(http.StatusOK))

			stored := placementsForPlaylist("pls-1")
			Expect(stored).To(HaveLen(1))
			Expect(*stored[0].SortOrder).To(Equal(8))
		})

		It("places a playlist at the root via the reserved 'root' segment", func() {
			recorder := env.call("PUT", "/playlist/folder/root/playlist/pls-1", `{"sortOrder":2}`)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			stored := placementsForPlaylist("pls-1")
			Expect(stored).To(HaveLen(1))
			Expect(stored[0].FolderID).To(Equal(model.PlaylistFolderRootID))
			Expect(*stored[0].SortOrder).To(Equal(2))
		})

		It("keeps the same playlist orderable in several folders at once", func() {
			firstFolder := env.seedFolder("Genre", "")
			secondFolder := env.seedFolder("Mood", "")

			Expect(env.call("PUT", "/playlist/folder/"+firstFolder.ID+"/playlist/pls-1", `{"sortOrder":1}`).Code).To(Equal(http.StatusOK))
			Expect(env.call("PUT", "/playlist/folder/"+secondFolder.ID+"/playlist/pls-1", `{"sortOrder":7}`).Code).To(Equal(http.StatusOK))

			sortOrderByFolderID := map[string]int{}
			for _, placement := range placementsForPlaylist("pls-1") {
				sortOrderByFolderID[placement.FolderID] = *placement.SortOrder
			}
			Expect(sortOrderByFolderID).To(HaveLen(2))
			Expect(sortOrderByFolderID[firstFolder.ID]).To(Equal(1))
			Expect(sortOrderByFolderID[secondFolder.ID]).To(Equal(7))
		})

		It("returns 404 for an unknown or foreign folder", func() {
			folder := env.seedFolder("Foreign", "")
			Expect(env.call("PUT", "/playlist/folder/nope/playlist/pls-1").Code).To(Equal(http.StatusNotFound))

			env.folderRepo.switchUser("user-2")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusNotFound))
		})

		It("rejects a malformed body with 400", func() {
			folder := env.seedFolder("Favorites", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1", `not json`).Code).
				To(Equal(http.StatusBadRequest))
		})
	})

	Describe("DELETE /playlist/folder/{id}/playlist/{playlistId}", func() {
		It("removes a placement and is idempotent", func() {
			folder := env.seedFolder("Favorites", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))

			Expect(env.call("DELETE", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))
			Expect(placementsForPlaylist("pls-1")).To(BeEmpty())

			// Removing a placement that does not exist still succeeds
			Expect(env.call("DELETE", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))
			Expect(env.call("DELETE", "/playlist/folder/root/playlist/pls-unknown").Code).To(Equal(http.StatusOK))
		})

		It("returns 404 for an unknown folder", func() {
			Expect(env.call("DELETE", "/playlist/folder/nope/playlist/pls-1").Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PUT /playlist/{playlistId}/placements (set placements)", func() {
		It("replaces every placement of the playlist", func() {
			firstFolder := env.seedFolder("First", "")
			secondFolder := env.seedFolder("Second", "")
			Expect(env.call("PUT", "/playlist/folder/"+firstFolder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))

			body := `{"placements":[{"folderId":"` + secondFolder.ID + `","sortOrder":4},{"folderId":"","sortOrder":1}]}`
			recorder := env.call("PUT", "/playlist/pls-1/placements", body)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var response playlistPlacementsResponse
			Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
			Expect(response.PlaylistID).To(Equal("pls-1"))
			Expect(response.Placements).To(HaveLen(2))

			sortOrderByFolderID := map[string]int{}
			for _, placement := range placementsForPlaylist("pls-1") {
				sortOrderByFolderID[placement.FolderID] = *placement.SortOrder
			}
			Expect(sortOrderByFolderID).To(HaveLen(2))
			Expect(sortOrderByFolderID[secondFolder.ID]).To(Equal(4))
			Expect(sortOrderByFolderID[model.PlaylistFolderRootID]).To(Equal(1))
		})

		It("unfiles the playlist back to the implicit root with an empty list", func() {
			folder := env.seedFolder("First", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1").Code).To(Equal(http.StatusOK))

			Expect(env.call("PUT", "/playlist/pls-1/placements", `{"placements":[]}`).Code).To(Equal(http.StatusOK))
			Expect(placementsForPlaylist("pls-1")).To(BeEmpty())
		})

		It("accepts the reserved 'root' folder id in the body", func() {
			Expect(env.call("PUT", "/playlist/pls-1/placements", `{"placements":[{"folderId":"root"}]}`).Code).
				To(Equal(http.StatusOK))

			stored := placementsForPlaylist("pls-1")
			Expect(stored).To(HaveLen(1))
			Expect(stored[0].FolderID).To(Equal(model.PlaylistFolderRootID))
		})

		It("leaves other playlists' placements alone", func() {
			folder := env.seedFolder("First", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-2").Code).To(Equal(http.StatusOK))

			Expect(env.call("PUT", "/playlist/pls-1/placements", `{"placements":[]}`).Code).To(Equal(http.StatusOK))
			Expect(placementsForPlaylist("pls-2")).To(HaveLen(1))
		})

		It("returns 404 when a folder in the list is unknown or foreign", func() {
			body := `{"placements":[{"folderId":"nope"}]}`
			Expect(env.call("PUT", "/playlist/pls-1/placements", body).Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 when the placements list is missing or the body is malformed", func() {
			Expect(env.call("PUT", "/playlist/pls-1/placements", `{}`).Code).To(Equal(http.StatusBadRequest))
			Expect(env.call("PUT", "/playlist/pls-1/placements", `oops`).Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Per-user isolation", func() {
		It("does not touch another user's placements", func() {
			folder := env.seedFolder("Mine", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID+"/playlist/pls-1", `{"sortOrder":1}`).Code).To(Equal(http.StatusOK))

			env.folderRepo.switchUser("user-2")
			Expect(env.call("PUT", "/playlist/folder/root/playlist/pls-1", `{"sortOrder":9}`).Code).To(Equal(http.StatusOK))
			secondUserPlacements := placementsForPlaylist("pls-1")
			Expect(secondUserPlacements).To(HaveLen(1))
			Expect(secondUserPlacements[0].FolderID).To(Equal(model.PlaylistFolderRootID))

			env.folderRepo.switchUser(testUserID)
			firstUserPlacements := placementsForPlaylist("pls-1")
			Expect(firstUserPlacements).To(HaveLen(1))
			Expect(firstUserPlacements[0].FolderID).To(Equal(folder.ID))
			Expect(*firstUserPlacements[0].SortOrder).To(Equal(1))
		})
	})

	Describe("Authentication", func() {
		It("requires authentication on the placement routes", func() {
			Expect(env.callUnauthenticated("PUT", "/playlist/folder/some-id/playlist/pls-1").Code).
				To(Equal(http.StatusUnauthorized))
			Expect(env.callUnauthenticated("DELETE", "/playlist/folder/some-id/playlist/pls-1").Code).
				To(Equal(http.StatusUnauthorized))
			Expect(env.callUnauthenticated("PUT", "/playlist/pls-1/placements").Code).
				To(Equal(http.StatusUnauthorized))
		})
	})
})
