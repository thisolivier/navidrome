package persistence

import (
	"time"

	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pocketbase/dbx"
)

func intPointer(value int) *int {
	return &value
}

var _ = Describe("PlaylistFolderRepository", func() {
	var repo model.PlaylistFolderRepository
	var regularUserRepo model.PlaylistFolderRepository
	var createdFolderIDs []string

	newRepoForUser := func(user model.User) model.PlaylistFolderRepository {
		ctx := log.NewContext(GinkgoT().Context())
		ctx = request.WithUser(ctx, user)
		return NewPlaylistFolderRepository(ctx, GetDBXBuilder())
	}

	trackFolder := func(folder *model.PlaylistFolder) {
		createdFolderIDs = append(createdFolderIDs, folder.ID)
	}

	createFolder := func(name string, parentID string) *model.PlaylistFolder {
		folder := &model.PlaylistFolder{Name: name, ParentID: parentID}
		Expect(repo.Put(folder)).To(Succeed())
		trackFolder(folder)
		return folder
	}

	placementsFor := func(currentRepo model.PlaylistFolderRepository, playlistID string) model.PlaylistPlacements {
		allPlacements, err := currentRepo.GetPlacements()
		Expect(err).ToNot(HaveOccurred())
		matching := model.PlaylistPlacements{}
		for _, placement := range allPlacements {
			if placement.PlaylistID == playlistID {
				matching = append(matching, placement)
			}
		}
		return matching
	}

	BeforeEach(func() {
		repo = newRepoForUser(adminUser)
		regularUserRepo = newRepoForUser(regularUser)
		createdFolderIDs = nil
	})

	AfterEach(func() {
		// Clean up in reverse order so children are removed before their parents
		for index := len(createdFolderIDs) - 1; index >= 0; index-- {
			_ = repo.Delete(createdFolderIDs[index])
			_ = regularUserRepo.Delete(createdFolderIDs[index])
		}
		createdFolderIDs = nil
		for _, playlistID := range []string{plsBest.ID, plsCool.ID} {
			Expect(repo.SetPlacements(playlistID, model.PlaylistPlacements{})).To(Succeed())
			Expect(regularUserRepo.SetPlacements(playlistID, model.PlaylistPlacements{})).To(Succeed())
		}
	})

	Describe("Folder CRUD", func() {
		It("creates a folder with generated ID, timestamps, and owner from context", func() {
			beforeCreate := time.Now().Add(-time.Second)
			folder := createFolder("Test Folder", "")

			Expect(folder.ID).ToNot(BeEmpty())
			Expect(folder.OwnerID).To(Equal(adminUser.ID))
			Expect(folder.CreatedAt).To(BeTemporally(">=", beforeCreate))
			Expect(folder.UpdatedAt).To(BeTemporally(">=", beforeCreate))
		})

		It("gets a folder by ID", func() {
			folder := createFolder("Get Test", "")

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
			folder := createFolder("Original Name", "")
			originalUpdatedAt := folder.UpdatedAt
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

		It("returns ErrNotFound when deleting a folder that does not exist", func() {
			Expect(repo.Delete("non-existent-id")).To(MatchError(model.ErrNotFound))
		})

		It("GetChildren returns only direct children of a folder", func() {
			parentFolder := createFolder("Parent", "")
			firstChild := createFolder("Child 1", parentFolder.ID)
			secondChild := createFolder("Child 2", parentFolder.ID)

			children, err := repo.GetChildren(parentFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(children).To(HaveLen(2))
			Expect([]string{children[0].ID, children[1].ID}).To(ConsistOf(firstChild.ID, secondChild.ID))
		})
	})

	Describe("Folder sort order", func() {
		It("round-trips a sort order and allows duplicates among siblings", func() {
			firstFolder := &model.PlaylistFolder{Name: "First", SortOrder: intPointer(3)}
			Expect(repo.Put(firstFolder)).To(Succeed())
			trackFolder(firstFolder)
			secondFolder := &model.PlaylistFolder{Name: "Second", SortOrder: intPointer(3)}
			Expect(repo.Put(secondFolder)).To(Succeed())
			trackFolder(secondFolder)

			retrievedFirst, err := repo.Get(firstFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedFirst.SortOrder).ToNot(BeNil())
			Expect(*retrievedFirst.SortOrder).To(Equal(3))

			retrievedSecond, err := repo.Get(secondFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*retrievedSecond.SortOrder).To(Equal(3))
		})

		It("stores an unset sort order as NULL and can clear it again", func() {
			folder := createFolder("Unordered", "")
			retrieved, err := repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.SortOrder).To(BeNil())

			folder.SortOrder = intPointer(7)
			Expect(repo.Put(folder)).To(Succeed())
			retrieved, err = repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*retrieved.SortOrder).To(Equal(7))

			folder.SortOrder = nil
			Expect(repo.Put(folder)).To(Succeed())
			retrieved, err = repo.Get(folder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.SortOrder).To(BeNil())
		})
	})

	Describe("Deleting a folder releases its contents", func() {
		It("re-parents children to the deleted folder's parent", func() {
			grandparent := createFolder("Grandparent", "")
			parent := &model.PlaylistFolder{Name: "Parent", ParentID: grandparent.ID}
			Expect(repo.Put(parent)).To(Succeed())
			child := createFolder("Child", parent.ID)

			Expect(repo.Delete(parent.ID)).To(Succeed())

			retrievedChild, err := repo.Get(child.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedChild.ParentID).To(Equal(grandparent.ID))
		})

		It("re-parents children to the root when a root folder is deleted", func() {
			parent := &model.PlaylistFolder{Name: "Root Parent"}
			Expect(repo.Put(parent)).To(Succeed())
			child := createFolder("Promoted Child", parent.ID)

			Expect(repo.Delete(parent.ID)).To(Succeed())

			retrievedChild, err := repo.Get(child.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedChild.ParentID).To(Equal(""))
		})

		It("promotes the placements it held to its parent, keeping their sort order", func() {
			parentFolder := createFolder("Parent", "")
			doomedFolder := &model.PlaylistFolder{Name: "Doomed", ParentID: parentFolder.ID}
			Expect(repo.Put(doomedFolder)).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: doomedFolder.ID, SortOrder: intPointer(4),
			})).To(Succeed())

			Expect(repo.Delete(doomedFolder.ID)).To(Succeed())

			remaining := placementsFor(repo, plsBest.ID)
			Expect(remaining).To(HaveLen(1))
			Expect(remaining[0].FolderID).To(Equal(parentFolder.ID))
			Expect(*remaining[0].SortOrder).To(Equal(4))
		})

		It("promotes the placements of a root folder to explicit root placements", func() {
			doomedFolder := &model.PlaylistFolder{Name: "Doomed Root"}
			Expect(repo.Put(doomedFolder)).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: doomedFolder.ID, SortOrder: intPointer(7),
			})).To(Succeed())

			Expect(repo.Delete(doomedFolder.ID)).To(Succeed())

			remaining := placementsFor(repo, plsBest.ID)
			Expect(remaining).To(HaveLen(1))
			Expect(remaining[0].FolderID).To(Equal(model.PlaylistFolderRootID))
			Expect(*remaining[0].SortOrder).To(Equal(7))
		})

		It("leaves an existing placement in the parent untouched when promoting", func() {
			parentFolder := createFolder("Parent", "")
			doomedFolder := &model.PlaylistFolder{Name: "Doomed", ParentID: parentFolder.ID}
			Expect(repo.Put(doomedFolder)).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: parentFolder.ID, SortOrder: intPointer(1),
			})).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: doomedFolder.ID, SortOrder: intPointer(9),
			})).To(Succeed())

			Expect(repo.Delete(doomedFolder.ID)).To(Succeed())

			remaining := placementsFor(repo, plsBest.ID)
			Expect(remaining).To(HaveLen(1))
			Expect(remaining[0].FolderID).To(Equal(parentFolder.ID))
			Expect(*remaining[0].SortOrder).To(Equal(1))
		})

		It("leaves placements in other folders alone", func() {
			doomedFolder := &model.PlaylistFolder{Name: "Doomed"}
			Expect(repo.Put(doomedFolder)).To(Succeed())
			survivingFolder := createFolder("Surviving", "")

			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsCool.ID, FolderID: doomedFolder.ID})).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: survivingFolder.ID})).To(Succeed())

			Expect(repo.Delete(doomedFolder.ID)).To(Succeed())

			bestPlacements := placementsFor(repo, plsBest.ID)
			Expect(bestPlacements).To(HaveLen(1))
			Expect(bestPlacements[0].FolderID).To(Equal(survivingFolder.ID))
		})

		It("promotes every user's placements, not just the folder owner's", func() {
			// A folder is private to its owner, so only the owner can hold placements in it.
			// This guards the promotion query against dropping the user_id it copies.
			doomedFolder := &model.PlaylistFolder{Name: "Doomed Multi"}
			Expect(repo.Put(doomedFolder)).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: doomedFolder.ID})).To(Succeed())
			Expect(regularUserRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: model.PlaylistFolderRootID, SortOrder: intPointer(3),
			})).To(Succeed())

			Expect(repo.Delete(doomedFolder.ID)).To(Succeed())

			adminPlacements := placementsFor(repo, plsBest.ID)
			Expect(adminPlacements).To(HaveLen(1))
			Expect(adminPlacements[0].FolderID).To(Equal(model.PlaylistFolderRootID))

			regularPlacements := placementsFor(regularUserRepo, plsBest.ID)
			Expect(regularPlacements).To(HaveLen(1))
			Expect(*regularPlacements[0].SortOrder).To(Equal(3))
		})

		It("does NOT delete playlists when a folder is deleted", func() {
			folder := &model.PlaylistFolder{Name: "Folder Holding Playlists"}
			Expect(repo.Put(folder)).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: folder.ID})).To(Succeed())

			Expect(repo.Delete(folder.ID)).To(Succeed())

			ctx := log.NewContext(GinkgoT().Context())
			ctx = request.WithUser(ctx, adminUser)
			playlistRepo := NewPlaylistRepository(ctx, GetDBXBuilder())
			Expect(playlistRepo.Exists(plsBest.ID)).To(BeTrue())
		})
	})

	Describe("Placements", func() {
		It("places a playlist in several folders, ordered independently", func() {
			firstFolder := createFolder("Folder A", "")
			secondFolder := createFolder("Folder B", "")

			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: firstFolder.ID, SortOrder: intPointer(1),
			})).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: secondFolder.ID, SortOrder: intPointer(9),
			})).To(Succeed())

			placements := placementsFor(repo, plsBest.ID)
			Expect(placements).To(HaveLen(2))
			sortOrderByFolderID := map[string]int{}
			for _, placement := range placements {
				Expect(placement.SortOrder).ToNot(BeNil())
				sortOrderByFolderID[placement.FolderID] = *placement.SortOrder
			}
			Expect(sortOrderByFolderID[firstFolder.ID]).To(Equal(1))
			Expect(sortOrderByFolderID[secondFolder.ID]).To(Equal(9))
		})

		It("upserts: placing the same playlist twice updates the sort order", func() {
			folder := createFolder("Upsert Folder", "")

			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: folder.ID, SortOrder: intPointer(1),
			})).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: folder.ID, SortOrder: intPointer(42),
			})).To(Succeed())

			placements := placementsFor(repo, plsBest.ID)
			Expect(placements).To(HaveLen(1))
			Expect(*placements[0].SortOrder).To(Equal(42))
		})

		It("stores an explicit root placement with an empty folder ID", func() {
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsCool.ID, FolderID: model.PlaylistFolderRootID, SortOrder: intPointer(2),
			})).To(Succeed())

			placements := placementsFor(repo, plsCool.ID)
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].FolderID).To(Equal(""))
			Expect(*placements[0].SortOrder).To(Equal(2))
		})

		It("keeps a nil sort order as unordered", func() {
			folder := createFolder("Unordered Placement", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: folder.ID})).To(Succeed())

			placements := placementsFor(repo, plsBest.ID)
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].SortOrder).To(BeNil())
		})

		It("DeletePlacement removes one placement and is idempotent", func() {
			folder := createFolder("Remove Placement", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: folder.ID})).To(Succeed())

			Expect(repo.DeletePlacement(plsBest.ID, folder.ID)).To(Succeed())
			Expect(placementsFor(repo, plsBest.ID)).To(BeEmpty())

			// Removing it again is not an error
			Expect(repo.DeletePlacement(plsBest.ID, folder.ID)).To(Succeed())
		})

		It("SetPlacements replaces every placement of the playlist", func() {
			firstFolder := createFolder("Set A", "")
			secondFolder := createFolder("Set B", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: firstFolder.ID})).To(Succeed())

			Expect(repo.SetPlacements(plsBest.ID, model.PlaylistPlacements{
				{FolderID: secondFolder.ID, SortOrder: intPointer(5)},
			})).To(Succeed())

			placements := placementsFor(repo, plsBest.ID)
			Expect(placements).To(HaveLen(1))
			Expect(placements[0].FolderID).To(Equal(secondFolder.ID))
			Expect(*placements[0].SortOrder).To(Equal(5))
		})

		It("SetPlacements keeps an overlapping placement and updates its sort order", func() {
			keptFolder := createFolder("Kept", "")
			droppedFolder := createFolder("Dropped", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: keptFolder.ID, SortOrder: intPointer(1),
			})).To(Succeed())
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: droppedFolder.ID,
			})).To(Succeed())

			Expect(repo.SetPlacements(plsBest.ID, model.PlaylistPlacements{
				{FolderID: keptFolder.ID, SortOrder: intPointer(6)},
				{FolderID: model.PlaylistFolderRootID, SortOrder: intPointer(2)},
			})).To(Succeed())

			sortOrderByFolderID := map[string]int{}
			for _, placement := range placementsFor(repo, plsBest.ID) {
				sortOrderByFolderID[placement.FolderID] = *placement.SortOrder
			}
			Expect(sortOrderByFolderID).To(HaveLen(2))
			Expect(sortOrderByFolderID[keptFolder.ID]).To(Equal(6))
			Expect(sortOrderByFolderID[model.PlaylistFolderRootID]).To(Equal(2))
		})

		It("SetPlacements with an empty list unfiles the playlist", func() {
			folder := createFolder("Set Empty", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{PlaylistID: plsBest.ID, FolderID: folder.ID})).To(Succeed())

			Expect(repo.SetPlacements(plsBest.ID, model.PlaylistPlacements{})).To(Succeed())

			Expect(placementsFor(repo, plsBest.ID)).To(BeEmpty())
		})

		It("keeps placements private to each user", func() {
			adminFolder := createFolder("Admin Folder", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: adminFolder.ID, SortOrder: intPointer(1),
			})).To(Succeed())
			Expect(regularUserRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsBest.ID, FolderID: model.PlaylistFolderRootID, SortOrder: intPointer(8),
			})).To(Succeed())

			adminPlacements := placementsFor(repo, plsBest.ID)
			Expect(adminPlacements).To(HaveLen(1))
			Expect(adminPlacements[0].FolderID).To(Equal(adminFolder.ID))

			regularPlacements := placementsFor(regularUserRepo, plsBest.ID)
			Expect(regularPlacements).To(HaveLen(1))
			Expect(regularPlacements[0].FolderID).To(Equal(""))
			Expect(*regularPlacements[0].SortOrder).To(Equal(8))
		})

		It("does not disturb another user's placements when replacing its own", func() {
			Expect(regularUserRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: plsCool.ID, FolderID: model.PlaylistFolderRootID,
			})).To(Succeed())

			Expect(repo.SetPlacements(plsCool.ID, model.PlaylistPlacements{})).To(Succeed())

			Expect(placementsFor(regularUserRepo, plsCool.ID)).To(HaveLen(1))
		})
	})

	Describe("Playlist lifecycle", func() {
		var playlistRepo model.PlaylistRepository

		countPlacementRows := func(playlistID string) int {
			var rowCount int
			err := GetDBXBuilder().
				NewQuery("select count(*) from playlist_placement where playlist_id = {:playlistID}").
				Bind(dbx.Params{"playlistID": playlistID}).
				Row(&rowCount)
			Expect(err).ToNot(HaveOccurred())
			return rowCount
		}

		// plantOrphanPlacement writes a placement for a playlist that does not exist. The test
		// database enables PRAGMA foreign_keys, so it takes a dedicated connection with the
		// pragma turned off — which is exactly the state a production connection may be in.
		plantOrphanPlacement := func(playlistID string) {
			ctx := GinkgoT().Context()
			connection, err := db.Db().Conn(ctx)
			Expect(err).ToNot(HaveOccurred())
			defer func() { Expect(connection.Close()).To(Succeed()) }()

			_, err = connection.ExecContext(ctx, "PRAGMA foreign_keys=off")
			Expect(err).ToNot(HaveOccurred())
			_, err = connection.ExecContext(ctx,
				"insert into playlist_placement (user_id, playlist_id, folder_id, sort_order) values (?, ?, '', null)",
				adminUser.ID, playlistID)
			Expect(err).ToNot(HaveOccurred())
			_, err = connection.ExecContext(ctx, "PRAGMA foreign_keys=on")
			Expect(err).ToNot(HaveOccurred())
		}

		BeforeEach(func() {
			ctx := log.NewContext(GinkgoT().Context())
			ctx = request.WithUser(ctx, adminUser)
			playlistRepo = NewPlaylistRepository(ctx, GetDBXBuilder())
		})

		It("removes every user's placements when the playlist is deleted", func() {
			temporaryPlaylist := model.Playlist{Name: "Temporary", OwnerID: adminUser.ID, OwnerName: adminUser.UserName}
			Expect(playlistRepo.Put(&temporaryPlaylist)).To(Succeed())
			folder := createFolder("Holder", "")

			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: temporaryPlaylist.ID, FolderID: folder.ID,
			})).To(Succeed())
			Expect(regularUserRepo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: temporaryPlaylist.ID, FolderID: model.PlaylistFolderRootID,
			})).To(Succeed())
			Expect(countPlacementRows(temporaryPlaylist.ID)).To(Equal(2))

			Expect(playlistRepo.Delete(temporaryPlaylist.ID)).To(Succeed())

			Expect(placementsFor(repo, temporaryPlaylist.ID)).To(BeEmpty())
			Expect(placementsFor(regularUserRepo, temporaryPlaylist.ID)).To(BeEmpty())
			// The rows are really gone, not merely filtered out of the read
			Expect(countPlacementRows(temporaryPlaylist.ID)).To(BeZero())
		})

		It("keeps placements when the playlist row survives the delete attempt", func() {
			privatePlaylist := model.Playlist{Name: "Private", OwnerID: adminUser.ID, OwnerName: adminUser.UserName}
			Expect(playlistRepo.Put(&privatePlaylist)).To(Succeed())
			DeferCleanup(func() {
				Expect(playlistRepo.Delete(privatePlaylist.ID)).To(Succeed())
			})
			folder := createFolder("Untouched", "")
			Expect(repo.PutPlacement(model.PlaylistPlacement{
				PlaylistID: privatePlaylist.ID, FolderID: folder.ID,
			})).To(Succeed())

			// regularUser cannot delete an admin-owned private playlist, so nothing is removed
			regularCtx := log.NewContext(GinkgoT().Context())
			regularCtx = request.WithUser(regularCtx, regularUser)
			Expect(NewPlaylistRepository(regularCtx, GetDBXBuilder()).Delete(privatePlaylist.ID)).To(Succeed())

			Expect(placementsFor(repo, privatePlaylist.ID)).To(HaveLen(1))
			Expect(playlistRepo.Exists(privatePlaylist.ID)).To(BeTrue())
		})

		It("never surfaces a placement of a playlist that does not exist", func() {
			orphanPlaylistID := "playlist-that-never-existed"
			plantOrphanPlacement(orphanPlaylistID)
			DeferCleanup(func() {
				Expect(repo.DeletePlacement(orphanPlaylistID, model.PlaylistFolderRootID)).To(Succeed())
			})
			Expect(countPlacementRows(orphanPlaylistID)).To(Equal(1))

			placements, err := repo.GetPlacements()
			Expect(err).ToNot(HaveOccurred())
			for _, placement := range placements {
				Expect(placement.PlaylistID).ToNot(Equal(orphanPlaylistID))
			}
		})
	})

	Describe("Owner isolation", func() {
		It("a user only sees their own folders", func() {
			adminFolder := createFolder("Admin Folder", "")
			regularFolder := &model.PlaylistFolder{Name: "Regular Folder"}
			Expect(regularUserRepo.Put(regularFolder)).To(Succeed())
			createdFolderIDs = append(createdFolderIDs, regularFolder.ID)

			regularFolders, err := regularUserRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			regularFolderIDs := make([]string, len(regularFolders))
			for index, folder := range regularFolders {
				regularFolderIDs[index] = folder.ID
			}
			Expect(regularFolderIDs).To(ContainElement(regularFolder.ID))
			Expect(regularFolderIDs).ToNot(ContainElement(adminFolder.ID))
		})

		It("an admin does not see other users' folders either — trees are private", func() {
			regularFolder := &model.PlaylistFolder{Name: "Regular Private"}
			Expect(regularUserRepo.Put(regularFolder)).To(Succeed())
			createdFolderIDs = append(createdFolderIDs, regularFolder.ID)

			_, err := repo.Get(regularFolder.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("a user cannot Get or delete a folder owned by another user", func() {
			adminFolder := createFolder("Admin Only", "")

			_, err := regularUserRepo.Get(adminFolder.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			Expect(regularUserRepo.Delete(adminFolder.ID)).To(MatchError(model.ErrNotFound))

			retrieved, err := repo.Get(adminFolder.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.Name).To(Equal("Admin Only"))
		})
	})
})
