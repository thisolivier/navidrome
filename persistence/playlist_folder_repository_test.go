package persistence

import (
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PlaylistFolderRepository", func() {
	var repo model.PlaylistFolderRepository
	var createdFolderIDs []string

	cleanupFolders := func() {
		// Clean up in reverse order to handle parent-child relationships
		for idx := len(createdFolderIDs) - 1; idx >= 0; idx-- {
			_ = repo.Delete(createdFolderIDs[idx])
		}
		createdFolderIDs = nil
	}

	trackFolder := func(folder *model.PlaylistFolder) {
		createdFolderIDs = append(createdFolderIDs, folder.ID)
	}

	BeforeEach(func() {
		ctx := log.NewContext(GinkgoT().Context())
		ctx = request.WithUser(ctx, adminUser)
		repo = NewPlaylistFolderRepository(ctx, GetDBXBuilder())
		createdFolderIDs = nil
	})

	AfterEach(func() {
		cleanupFolders()
	})

	Describe("CRUD Operations", func() {
		It("creates a folder with generated ID, timestamps, and owner from context", func() {
			beforeCreate := time.Now().Add(-time.Second)
			folder := &model.PlaylistFolder{Name: "Test Folder"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(folder.ID).ToNot(BeEmpty())
			Expect(folder.OwnerID).To(Equal(adminUser.ID))
			Expect(folder.CreatedAt).To(BeTemporally(">=", beforeCreate))
			Expect(folder.UpdatedAt).To(BeTemporally(">=", beforeCreate))
		})

		It("gets a folder by ID", func() {
			folder := &model.PlaylistFolder{Name: "Get Test"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			retrieved, err := repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal(folder.ID))
			Expect(retrieved.Name).To(Equal("Get Test"))
			Expect(retrieved.OwnerID).To(Equal(adminUser.ID))
		})

		It("returns ErrNotFound for a non-existent folder", func() {
			_, err := repo.Get("non-existent-id")
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("updates a folder name and refreshes UpdatedAt", func() {
			folder := &model.PlaylistFolder{Name: "Original Name"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			originalUpdatedAt := folder.UpdatedAt

			// Small delay to ensure timestamp difference
			time.Sleep(10 * time.Millisecond)

			folder.Name = "Updated Name"
			Expect(repo.Put(folder)).To(Succeed())

			retrieved, err := repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.Name).To(Equal("Updated Name"))
			Expect(retrieved.UpdatedAt).To(BeTemporally(">", originalUpdatedAt))
		})

		It("deletes a folder", func() {
			folder := &model.PlaylistFolder{Name: "To Delete"}
			Expect(repo.Put(folder)).To(Succeed())

			Expect(repo.Delete(folder.ID)).To(Succeed())

			_, err := repo.Get(folder.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("deleting a non-existent folder does not error", func() {
			// DELETE with no matching rows succeeds silently in the base repository
			Expect(repo.Delete("non-existent-id")).To(Succeed())
		})
	})

	Describe("Root and Children", func() {
		It("GetRoots returns only folders with empty parent_id", func() {
			rootFolder := &model.PlaylistFolder{Name: "Root Folder"}
			Expect(repo.Put(rootFolder)).To(Succeed())
			trackFolder(rootFolder)

			childFolder := &model.PlaylistFolder{Name: "Child Folder", ParentID: rootFolder.ID}
			Expect(repo.Put(childFolder)).To(Succeed())
			trackFolder(childFolder)

			roots, err := repo.GetRoots()
			Expect(err).ToNot(HaveOccurred())

			rootIDs := make([]string, len(roots))
			for idx, folder := range roots {
				rootIDs[idx] = folder.ID
			}
			Expect(rootIDs).To(ContainElement(rootFolder.ID))
			Expect(rootIDs).ToNot(ContainElement(childFolder.ID))
		})

		It("GetChildren returns only direct children of a folder", func() {
			parentFolder := &model.PlaylistFolder{Name: "Parent"}
			Expect(repo.Put(parentFolder)).To(Succeed())
			trackFolder(parentFolder)

			child1 := &model.PlaylistFolder{Name: "Child 1", ParentID: parentFolder.ID}
			Expect(repo.Put(child1)).To(Succeed())
			trackFolder(child1)

			child2 := &model.PlaylistFolder{Name: "Child 2", ParentID: parentFolder.ID}
			Expect(repo.Put(child2)).To(Succeed())
			trackFolder(child2)

			children, err := repo.GetChildren(parentFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(children).To(HaveLen(2))

			childIDs := []string{children[0].ID, children[1].ID}
			Expect(childIDs).To(ContainElements(child1.ID, child2.ID))
		})

		It("handles nested folders (3 levels) correctly", func() {
			grandparent := &model.PlaylistFolder{Name: "Grandparent"}
			Expect(repo.Put(grandparent)).To(Succeed())
			trackFolder(grandparent)

			parent := &model.PlaylistFolder{Name: "Parent", ParentID: grandparent.ID}
			Expect(repo.Put(parent)).To(Succeed())
			trackFolder(parent)

			child := &model.PlaylistFolder{Name: "Child", ParentID: parent.ID}
			Expect(repo.Put(child)).To(Succeed())
			trackFolder(child)

			// GetRoots should only return the grandparent
			roots, err := repo.GetRoots()
			Expect(err).ToNot(HaveOccurred())
			rootIDs := make([]string, len(roots))
			for idx, folder := range roots {
				rootIDs[idx] = folder.ID
			}
			Expect(rootIDs).To(ContainElement(grandparent.ID))
			Expect(rootIDs).ToNot(ContainElement(parent.ID))
			Expect(rootIDs).ToNot(ContainElement(child.ID))

			// GetChildren of grandparent should return only parent
			children, err := repo.GetChildren(grandparent.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(children).To(HaveLen(1))
			Expect(children[0].ID).To(Equal(parent.ID))

			// GetChildren of parent should return only child
			children, err = repo.GetChildren(parent.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(children).To(HaveLen(1))
			Expect(children[0].ID).To(Equal(child.ID))

			// GetChildren of child should return nothing
			children, err = repo.GetChildren(child.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(children).To(HaveLen(0))
		})
	})

	Describe("Cascade behavior on delete", func() {
		It("promotes children to root when parent is deleted", func() {
			parentFolder := &model.PlaylistFolder{Name: "Parent To Delete"}
			Expect(repo.Put(parentFolder)).To(Succeed())

			child1 := &model.PlaylistFolder{Name: "Orphan 1", ParentID: parentFolder.ID}
			Expect(repo.Put(child1)).To(Succeed())
			trackFolder(child1)

			child2 := &model.PlaylistFolder{Name: "Orphan 2", ParentID: parentFolder.ID}
			Expect(repo.Put(child2)).To(Succeed())
			trackFolder(child2)

			Expect(repo.Delete(parentFolder.ID)).To(Succeed())

			// Children should now be roots (parent_id = "")
			retrieved1, err := repo.Get(child1.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved1.ParentID).To(Equal(""))

			retrieved2, err := repo.Get(child2.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved2.ParentID).To(Equal(""))

			// They should appear in GetRoots
			roots, err := repo.GetRoots()
			Expect(err).ToNot(HaveOccurred())
			rootIDs := make([]string, len(roots))
			for idx, folder := range roots {
				rootIDs[idx] = folder.ID
			}
			Expect(rootIDs).To(ContainElements(child1.ID, child2.ID))
		})

		It("removes memberships when folder is deleted (FK CASCADE)", func() {
			folder := &model.PlaylistFolder{Name: "Folder With Memberships"}
			Expect(repo.Put(folder)).To(Succeed())

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder.ID, plsCool.ID)).To(Succeed())

			// Verify memberships exist
			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(HaveLen(2))

			// Delete the folder
			Expect(repo.Delete(folder.ID)).To(Succeed())

			// Memberships should be gone (folder no longer exists, so GetPlaylistIDs
			// would just return empty for that ID)
			playlistIDs, err = repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(HaveLen(0))
		})

		It("does NOT delete playlists when folder is deleted", func() {
			folder := &model.PlaylistFolder{Name: "Folder Holding Playlists"}
			Expect(repo.Put(folder)).To(Succeed())

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())

			// Delete the folder
			Expect(repo.Delete(folder.ID)).To(Succeed())

			// Playlist should still exist
			ctx := log.NewContext(GinkgoT().Context())
			ctx = request.WithUser(ctx, adminUser)
			playlistRepo := NewPlaylistRepository(ctx, GetDBXBuilder())
			Expect(playlistRepo.Exists(plsBest.ID)).To(BeTrue())
		})
	})

	Describe("Many-to-many (playlist <-> folder)", func() {
		It("AddPlaylist adds a membership", func() {
			folder := &model.PlaylistFolder{Name: "Membership Test"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())

			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(ConsistOf(plsBest.ID))
		})

		It("a playlist can be in multiple folders", func() {
			folder1 := &model.PlaylistFolder{Name: "Folder A"}
			Expect(repo.Put(folder1)).To(Succeed())
			trackFolder(folder1)

			folder2 := &model.PlaylistFolder{Name: "Folder B"}
			Expect(repo.Put(folder2)).To(Succeed())
			trackFolder(folder2)

			Expect(repo.AddPlaylist(folder1.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder2.ID, plsBest.ID)).To(Succeed())

			folders, err := repo.GetPlaylistFolders(plsBest.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(folders).To(HaveLen(2))

			folderIDs := []string{folders[0].ID, folders[1].ID}
			Expect(folderIDs).To(ContainElements(folder1.ID, folder2.ID))
		})

		It("a folder can contain multiple playlists", func() {
			folder := &model.PlaylistFolder{Name: "Multi Playlist Folder"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder.ID, plsCool.ID)).To(Succeed())

			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(ConsistOf(plsBest.ID, plsCool.ID))
		})

		It("RemovePlaylist removes the membership", func() {
			folder := &model.PlaylistFolder{Name: "Remove Test"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder.ID, plsCool.ID)).To(Succeed())

			Expect(repo.RemovePlaylist(folder.ID, plsBest.ID)).To(Succeed())

			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(ConsistOf(plsCool.ID))
		})

		It("GetPlaylistFolders returns all folders containing a playlist", func() {
			folder1 := &model.PlaylistFolder{Name: "Contains Best 1"}
			Expect(repo.Put(folder1)).To(Succeed())
			trackFolder(folder1)

			folder2 := &model.PlaylistFolder{Name: "Contains Best 2"}
			Expect(repo.Put(folder2)).To(Succeed())
			trackFolder(folder2)

			emptyFolder := &model.PlaylistFolder{Name: "Empty Folder"}
			Expect(repo.Put(emptyFolder)).To(Succeed())
			trackFolder(emptyFolder)

			Expect(repo.AddPlaylist(folder1.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder2.ID, plsBest.ID)).To(Succeed())

			folders, err := repo.GetPlaylistFolders(plsBest.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(folders).To(HaveLen(2))

			folderIDs := []string{folders[0].ID, folders[1].ID}
			Expect(folderIDs).To(ContainElements(folder1.ID, folder2.ID))
			Expect(folderIDs).ToNot(ContainElement(emptyFolder.ID))
		})

		It("GetPlaylistIDs returns all playlist IDs in a folder", func() {
			folder := &model.PlaylistFolder{Name: "Full Folder"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())
			Expect(repo.AddPlaylist(folder.ID, plsCool.ID)).To(Succeed())

			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(ConsistOf(plsBest.ID, plsCool.ID))
		})
	})

	Describe("Owner isolation", func() {
		var adminRepo model.PlaylistFolderRepository
		var regularRepo model.PlaylistFolderRepository

		BeforeEach(func() {
			adminCtx := log.NewContext(GinkgoT().Context())
			adminCtx = request.WithUser(adminCtx, adminUser)
			adminRepo = NewPlaylistFolderRepository(adminCtx, GetDBXBuilder())

			regularCtx := log.NewContext(GinkgoT().Context())
			regularCtx = request.WithUser(regularCtx, regularUser)
			regularRepo = NewPlaylistFolderRepository(regularCtx, GetDBXBuilder())
		})

		It("regular user can only see their own folders", func() {
			adminFolder := &model.PlaylistFolder{Name: "Admin Folder"}
			Expect(adminRepo.Put(adminFolder)).To(Succeed())
			trackFolder(adminFolder)

			regularFolder := &model.PlaylistFolder{Name: "Regular Folder"}
			Expect(regularRepo.Put(regularFolder)).To(Succeed())
			createdFolderIDs = append(createdFolderIDs, regularFolder.ID)

			// Regular user should only see their folder
			allFolders, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())

			folderIDs := make([]string, len(allFolders))
			for idx, folder := range allFolders {
				folderIDs[idx] = folder.ID
			}
			Expect(folderIDs).To(ContainElement(regularFolder.ID))
			Expect(folderIDs).ToNot(ContainElement(adminFolder.ID))
		})

		It("admin user can see all folders", func() {
			adminFolder := &model.PlaylistFolder{Name: "Admin Visible"}
			Expect(adminRepo.Put(adminFolder)).To(Succeed())
			trackFolder(adminFolder)

			regularFolder := &model.PlaylistFolder{Name: "Regular Visible"}
			Expect(regularRepo.Put(regularFolder)).To(Succeed())
			createdFolderIDs = append(createdFolderIDs, regularFolder.ID)

			// Admin should see both
			allFolders, err := adminRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())

			folderIDs := make([]string, len(allFolders))
			for idx, folder := range allFolders {
				folderIDs[idx] = folder.ID
			}
			Expect(folderIDs).To(ContainElements(adminFolder.ID, regularFolder.ID))
		})

		It("regular user cannot Get a folder owned by another user", func() {
			adminFolder := &model.PlaylistFolder{Name: "Admin Only"}
			Expect(adminRepo.Put(adminFolder)).To(Succeed())
			trackFolder(adminFolder)

			_, err := regularRepo.Get(adminFolder.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("regular user cannot delete a folder owned by another user", func() {
			adminFolder := &model.PlaylistFolder{Name: "Admin Protected"}
			Expect(adminRepo.Put(adminFolder)).To(Succeed())
			trackFolder(adminFolder)

			// Attempt to delete — the ownerFilter prevents it
			_ = regularRepo.Delete(adminFolder.ID)

			// Folder should still exist for admin
			retrieved, err := adminRepo.Get(adminFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.Name).To(Equal("Admin Protected"))
		})
	})

	Describe("Edge cases", func() {
		It("creates a folder with parent_id referencing an existing folder", func() {
			parentFolder := &model.PlaylistFolder{Name: "Existing Parent"}
			Expect(repo.Put(parentFolder)).To(Succeed())
			trackFolder(parentFolder)

			childFolder := &model.PlaylistFolder{Name: "Child With Parent", ParentID: parentFolder.ID}
			Expect(repo.Put(childFolder)).To(Succeed())
			trackFolder(childFolder)

			retrieved, err := repo.Get(childFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ParentID).To(Equal(parentFolder.ID))
		})

		It("deleting a deeply nested folder only promotes direct children", func() {
			grandparent := &model.PlaylistFolder{Name: "GP"}
			Expect(repo.Put(grandparent)).To(Succeed())
			trackFolder(grandparent)

			parent := &model.PlaylistFolder{Name: "P", ParentID: grandparent.ID}
			Expect(repo.Put(parent)).To(Succeed())

			child := &model.PlaylistFolder{Name: "C", ParentID: parent.ID}
			Expect(repo.Put(child)).To(Succeed())
			trackFolder(child)

			// Delete the middle (parent) — child should be promoted to root
			Expect(repo.Delete(parent.ID)).To(Succeed())

			// Child should now have empty parent_id (promoted to root)
			retrieved, err := repo.Get(child.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ParentID).To(Equal(""))

			// Grandparent should still exist unchanged
			retrievedGP, err := repo.Get(grandparent.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedGP.Name).To(Equal("GP"))
		})

		It("removing the last membership from a folder leaves the folder intact", func() {
			folder := &model.PlaylistFolder{Name: "Will Be Empty"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			Expect(repo.AddPlaylist(folder.ID, plsBest.ID)).To(Succeed())
			Expect(repo.RemovePlaylist(folder.ID, plsBest.ID)).To(Succeed())

			// Folder should still exist
			retrieved, err := repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.Name).To(Equal("Will Be Empty"))

			// No playlists in folder
			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(HaveLen(0))
		})

		It("GetPlaylistFolders returns empty for a playlist not in any folder", func() {
			folders, err := repo.GetPlaylistFolders(plsCool.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(folders).To(HaveLen(0))
		})

		It("GetPlaylistIDs returns empty for an empty folder", func() {
			folder := &model.PlaylistFolder{Name: "Empty Folder"}
			Expect(repo.Put(folder)).To(Succeed())
			trackFolder(folder)

			playlistIDs, err := repo.GetPlaylistIDs(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(playlistIDs).To(HaveLen(0))
		})
	})
})
