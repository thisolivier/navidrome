package nativeapi

import (
	"encoding/json"
	"net/http"

	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Playlist Folder Endpoints", func() {
	var env *playlistFolderTestEnv

	BeforeEach(func() {
		env = setupPlaylistFolderTestEnv()
	})

	decodeTree := func(response *http.Response) playlistFolderTreeResponse {
		var tree playlistFolderTreeResponse
		Expect(json.NewDecoder(response.Body).Decode(&tree)).To(Succeed())
		return tree
	}

	Describe("GET /playlist/folder (tree envelope)", func() {
		It("returns the v2 envelope with empty arrays when nothing is filed", func() {
			recorder := env.call("GET", "/playlist/folder")
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var raw map[string]any
			Expect(json.Unmarshal(recorder.Body.Bytes(), &raw)).To(Succeed())
			Expect(raw).To(HaveKeyWithValue("folderApiVersion", float64(2)))
			Expect(raw["folders"]).To(BeEmpty())
			Expect(raw["placements"]).To(BeEmpty())
			Expect(raw["folders"]).ToNot(BeNil())
			Expect(raw["placements"]).ToNot(BeNil())
		})

		It("returns folders and placements, including an explicit root placement", func() {
			parentFolder := env.seedFolder("DJ Sets", "")
			childFolder := env.seedFolder("2026", parentFolder.ID)
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-summer", FolderID: childFolder.ID, SortOrder: sortOrderPointer(3),
			})).To(Succeed())
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-road", FolderID: model.PlaylistFolderRootID, SortOrder: sortOrderPointer(2),
			})).To(Succeed())

			recorder := env.call("GET", "/playlist/folder")
			Expect(recorder.Code).To(Equal(http.StatusOK))
			tree := decodeTree(recorder.Result())

			Expect(tree.FolderAPIVersion).To(Equal(2))
			Expect(tree.Folders).To(HaveLen(2))
			Expect(tree.Placements).To(HaveLen(2))

			parentIDByFolderID := map[string]string{}
			for _, folder := range tree.Folders {
				parentIDByFolderID[folder.ID] = folder.ParentID
			}
			Expect(parentIDByFolderID[childFolder.ID]).To(Equal(parentFolder.ID))
			Expect(parentIDByFolderID[parentFolder.ID]).To(Equal(""))

			folderIDByPlaylistID := map[string]string{}
			for _, placement := range tree.Placements {
				folderIDByPlaylistID[placement.PlaylistID] = placement.FolderID
			}
			Expect(folderIDByPlaylistID["playlist-summer"]).To(Equal(childFolder.ID))
			Expect(folderIDByPlaylistID["playlist-road"]).To(Equal(""))
		})

		It("omits sortOrder when it is unset", func() {
			env.seedFolder("Unordered", "")
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{PlaylistID: "playlist-1"})).To(Succeed())

			recorder := env.call("GET", "/playlist/folder")
			var raw struct {
				Folders    []map[string]any `json:"folders"`
				Placements []map[string]any `json:"placements"`
			}
			Expect(json.Unmarshal(recorder.Body.Bytes(), &raw)).To(Succeed())
			Expect(raw.Folders[0]).ToNot(HaveKey("sortOrder"))
			Expect(raw.Folders[0]).To(HaveKey("createdAt"))
			Expect(raw.Folders[0]).To(HaveKey("updatedAt"))
			Expect(raw.Placements[0]).ToNot(HaveKey("sortOrder"))
		})

		It("does not leak another user's folders or placements", func() {
			env.seedFolder("Private", "")
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{PlaylistID: "playlist-1"})).To(Succeed())
			env.folderRepo.switchUser("user-2")

			tree := decodeTree(env.call("GET", "/playlist/folder").Result())
			Expect(tree.Folders).To(BeEmpty())
			Expect(tree.Placements).To(BeEmpty())
		})
	})

	Describe("POST /playlist/folder", func() {
		It("creates a folder and returns it", func() {
			recorder := env.call("POST", "/playlist/folder", `{"name":"Rock","sortOrder":4}`)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var created playlistFolderResponse
			Expect(json.Unmarshal(recorder.Body.Bytes(), &created)).To(Succeed())
			Expect(created.ID).To(HavePrefix("folder-"))
			Expect(created.Name).To(Equal("Rock"))
			Expect(created.ParentID).To(Equal(""))
			Expect(*created.SortOrder).To(Equal(4))

			stored, err := env.folderRepo.Get(created.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Name).To(Equal("Rock"))
		})

		It("creates a nested folder under an existing parent", func() {
			parentFolder := env.seedFolder("Parent", "")

			recorder := env.call("POST", "/playlist/folder", `{"name":"Child","parentId":"`+parentFolder.ID+`"}`)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var created playlistFolderResponse
			Expect(json.Unmarshal(recorder.Body.Bytes(), &created)).To(Succeed())
			Expect(created.ParentID).To(Equal(parentFolder.ID))
			Expect(created.SortOrder).To(BeNil())
		})

		It("rejects a missing name with 400", func() {
			Expect(env.call("POST", "/playlist/folder", `{}`).Code).To(Equal(http.StatusBadRequest))
			Expect(env.call("POST", "/playlist/folder", `{"name":""}`).Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 404 for an unknown parent", func() {
			recorder := env.call("POST", "/playlist/folder", `{"name":"Orphan","parentId":"nope"}`)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PUT /playlist/folder/{id}", func() {
		It("renames a folder", func() {
			folder := env.seedFolder("Old Name", "")

			recorder := env.call("PUT", "/playlist/folder/"+folder.ID, `{"name":"New Name"}`)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			stored, err := env.folderRepo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Name).To(Equal("New Name"))
		})

		It("re-parents a folder and can move it back to the root", func() {
			firstFolder := env.seedFolder("First", "")
			secondFolder := env.seedFolder("Second", "")

			Expect(env.call("PUT", "/playlist/folder/"+secondFolder.ID,
				`{"parentId":"`+firstFolder.ID+`"}`).Code).To(Equal(http.StatusOK))
			stored, err := env.folderRepo.Get(secondFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.ParentID).To(Equal(firstFolder.ID))

			Expect(env.call("PUT", "/playlist/folder/"+secondFolder.ID, `{"parentId":""}`).Code).To(Equal(http.StatusOK))
			stored, err = env.folderRepo.Get(secondFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.ParentID).To(Equal(""))
		})

		It("sets and clears the sort order", func() {
			folder := env.seedFolder("Sortable", "")

			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"sortOrder":9}`).Code).To(Equal(http.StatusOK))
			stored, err := env.folderRepo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*stored.SortOrder).To(Equal(9))

			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"sortOrder":null}`).Code).To(Equal(http.StatusOK))
			stored, err = env.folderRepo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.SortOrder).To(BeNil())

			// An omitted sortOrder leaves the stored value untouched
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"sortOrder":5}`).Code).To(Equal(http.StatusOK))
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"name":"Renamed"}`).Code).To(Equal(http.StatusOK))
			stored, err = env.folderRepo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*stored.SortOrder).To(Equal(5))
		})

		It("rejects making a folder its own parent with 400", func() {
			folder := env.seedFolder("Self", "")

			recorder := env.call("PUT", "/playlist/folder/"+folder.ID, `{"parentId":"`+folder.ID+`"}`)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})

		It("rejects moving a folder under its own descendant with 400", func() {
			grandparent := env.seedFolder("Grandparent", "")
			parent := env.seedFolder("Parent", grandparent.ID)
			child := env.seedFolder("Child", parent.ID)

			recorder := env.call("PUT", "/playlist/folder/"+grandparent.ID, `{"parentId":"`+child.ID+`"}`)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			stored, err := env.folderRepo.Get(grandparent.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.ParentID).To(Equal(""))
		})

		It("returns 404 for an unknown folder or unknown parent", func() {
			folder := env.seedFolder("Known", "")

			Expect(env.call("PUT", "/playlist/folder/nope", `{"name":"x"}`).Code).To(Equal(http.StatusNotFound))
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"parentId":"nope"}`).Code).To(Equal(http.StatusNotFound))
		})

		It("returns 404 when the folder belongs to another user", func() {
			folder := env.seedFolder("Foreign", "")
			env.folderRepo.switchUser("user-2")

			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"name":"x"}`).Code).To(Equal(http.StatusNotFound))
		})

		It("rejects an empty name with 400", func() {
			folder := env.seedFolder("Named", "")
			Expect(env.call("PUT", "/playlist/folder/"+folder.ID, `{"name":""}`).Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("DELETE /playlist/folder/{id}", func() {
		It("deletes the folder and lifts its children and placements to its parent", func() {
			grandparent := env.seedFolder("Grandparent", "")
			parent := env.seedFolder("Parent", grandparent.ID)
			child := env.seedFolder("Child", parent.ID)
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-1", FolderID: parent.ID, SortOrder: sortOrderPointer(5),
			})).To(Succeed())

			recorder := env.call("DELETE", "/playlist/folder/"+parent.ID)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			_, err := env.folderRepo.Get(parent.ID)
			Expect(err).To(MatchError(model.ErrNotFound))

			storedChild, err := env.folderRepo.Get(child.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(storedChild.ParentID).To(Equal(grandparent.ID))

			placements, err := env.folderRepo.GetPlacements()
			Expect(err).ToNot(HaveOccurred())
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].FolderID).To(Equal(grandparent.ID))
			Expect(*placements[0].SortOrder).To(Equal(5))
		})

		It("keeps a placement the parent already had when lifting", func() {
			grandparent := env.seedFolder("Grandparent", "")
			parent := env.seedFolder("Parent", grandparent.ID)
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-1", FolderID: grandparent.ID, SortOrder: sortOrderPointer(1),
			})).To(Succeed())
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-1", FolderID: parent.ID, SortOrder: sortOrderPointer(9),
			})).To(Succeed())

			Expect(env.call("DELETE", "/playlist/folder/"+parent.ID).Code).To(Equal(http.StatusOK))

			placements, err := env.folderRepo.GetPlacements()
			Expect(err).ToNot(HaveOccurred())
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].FolderID).To(Equal(grandparent.ID))
			Expect(*placements[0].SortOrder).To(Equal(1))
		})

		It("lifts the placements of a root folder to explicit root placements", func() {
			folder := env.seedFolder("Root Folder", "")
			Expect(env.folderRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: "playlist-1", FolderID: folder.ID, SortOrder: sortOrderPointer(2),
			})).To(Succeed())

			Expect(env.call("DELETE", "/playlist/folder/"+folder.ID).Code).To(Equal(http.StatusOK))

			placements, err := env.folderRepo.GetPlacements()
			Expect(err).ToNot(HaveOccurred())
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].FolderID).To(Equal(model.PlaylistFolderRootID))
			Expect(*placements[0].SortOrder).To(Equal(2))
		})

		It("returns 404 for an unknown or foreign folder", func() {
			folder := env.seedFolder("Foreign", "")
			Expect(env.call("DELETE", "/playlist/folder/nope").Code).To(Equal(http.StatusNotFound))

			env.folderRepo.switchUser("user-2")
			Expect(env.call("DELETE", "/playlist/folder/"+folder.ID).Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Authentication", func() {
		It("requires authentication on every folder route", func() {
			paths := map[string]string{
				"GET":    "/playlist/folder",
				"POST":   "/playlist/folder",
				"PUT":    "/playlist/folder/some-id",
				"DELETE": "/playlist/folder/some-id",
			}
			for method, path := range paths {
				Expect(env.callUnauthenticated(method, path).Code).To(Equal(http.StatusUnauthorized),
					"%s %s should require authentication", method, path)
			}
		})
	})
})
