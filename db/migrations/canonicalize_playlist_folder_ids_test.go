package migrations

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upCanonicalizePlaylistFolderIds", func() {
	var db *sql.DB
	ctx := context.Background()

	// Same fixture ids as uniform_canonical_ids_test.go, so the expected outputs are the
	// exact values upstream's migration wrote into playlist.id/user.id.
	const (
		hashID    = "5cLJPkLA5DK2BADhoeotPk" // canonical, kept
		randOld   = "zzzzzzzzzzzzzzzzzzzzzz" // overflows -> remapped
		randNew   = "3LyqmwQBm5IRqlVjNYASwb"
		legacyOld = "e3b7fc2ae9447bbec37a13bf916e3cf6" // 32-hex -> re-encoded
		legacyNew = "6VHl3uR4kss6sUPKA8Cwnk"
		uuidOld   = "f47ac10b-58cc-4372-a567-0e02b2c3d479" // uuid -> re-encoded
		uuidNew   = "7rke2SAWaicSeSYzkhww6R"
	)

	BeforeEach(func() {
		var err error
		db, err = sql.Open("sqlite3", "file::memory:")
		Expect(err).ToNot(HaveOccurred())
		db.SetMaxOpenConns(1) // non-shared :memory: — every new conn is a fresh empty DB
		DeferCleanup(func() { _ = db.Close() })
	})

	runMigration := func() {
		tx, err := db.Begin()
		Expect(err).ToNot(HaveOccurred())
		Expect(upCanonicalizePlaylistFolderIds(ctx, tx)).To(Succeed())
		Expect(tx.Commit()).To(Succeed())
	}

	get := func(query string) string {
		var s string
		ExpectWithOffset(1, db.QueryRow(query).Scan(&s)).To(Succeed())
		return s
	}

	It("no-ops cleanly when the fork tables do not exist", func() {
		runMigration()
	})

	Context("with pre-canonical fork data", func() {
		BeforeEach(func() {
			_, err := db.Exec(`
				CREATE TABLE playlist_folder (id text primary key, name text, parent_id text, owner_id text, sort_order integer);
				CREATE TABLE playlist_placement (user_id text, playlist_id text, folder_id text, sort_order integer,
					PRIMARY KEY (user_id, playlist_id, folder_id));
			`)
			Expect(err).ToNot(HaveOccurred())
			seed := func(query string, args ...any) {
				_, err := db.Exec(query, args...)
				ExpectWithOffset(1, err).ToNot(HaveOccurred())
			}
			// A root folder (legacy id, empty parent) owned by an old-shaped user id,
			// and a child folder whose parent reference must follow the parent's id.
			seed(`INSERT INTO playlist_folder VALUES (?, 'Root Folder', '', ?, 1)`, legacyOld, randOld)
			seed(`INSERT INTO playlist_folder VALUES (?, 'Child Folder', ?, ?, NULL)`, uuidOld, legacyOld, randOld)
			// A placement inside the folder and an explicit root placement (folder_id '').
			seed(`INSERT INTO playlist_placement VALUES (?, ?, ?, 3)`, randOld, uuidOld, legacyOld)
			seed(`INSERT INTO playlist_placement VALUES (?, ?, '', NULL)`, randOld, legacyOld)
			// Already-canonical ids must pass through untouched.
			seed(`INSERT INTO playlist_folder VALUES (?, 'Canonical', '', ?, NULL)`, hashID, hashID)
		})

		It("re-encodes every id column with the same transform upstream applied", func() {
			runMigration()

			Expect(get(`SELECT id FROM playlist_folder WHERE name='Root Folder'`)).To(Equal(legacyNew))
			Expect(get(`SELECT owner_id FROM playlist_folder WHERE name='Root Folder'`)).To(Equal(randNew))
			Expect(get(`SELECT parent_id FROM playlist_folder WHERE name='Root Folder'`)).To(Equal(""))
			Expect(get(`SELECT id FROM playlist_folder WHERE name='Child Folder'`)).To(Equal(uuidNew))
			Expect(get(`SELECT parent_id FROM playlist_folder WHERE name='Child Folder'`)).To(Equal(legacyNew))

			Expect(get(`SELECT user_id FROM playlist_placement WHERE folder_id <> ''`)).To(Equal(randNew))
			Expect(get(`SELECT playlist_id FROM playlist_placement WHERE folder_id <> ''`)).To(Equal(uuidNew))
			Expect(get(`SELECT folder_id FROM playlist_placement WHERE folder_id <> ''`)).To(Equal(legacyNew))
			// Root placement keeps its reserved empty folder_id and follows the playlist id.
			Expect(get(`SELECT playlist_id FROM playlist_placement WHERE folder_id = ''`)).To(Equal(legacyNew))

			Expect(get(`SELECT owner_id FROM playlist_folder WHERE name='Canonical'`)).To(Equal(hashID))
			Expect(get(`SELECT id FROM playlist_folder WHERE name='Canonical'`)).To(Equal(hashID))
		})

		It("is idempotent: a second run changes nothing", func() {
			runMigration()
			runMigration()
			Expect(get(`SELECT id FROM playlist_folder WHERE name='Root Folder'`)).To(Equal(legacyNew))
			Expect(get(`SELECT folder_id FROM playlist_placement WHERE folder_id <> ''`)).To(Equal(legacyNew))
		})
	})
})
