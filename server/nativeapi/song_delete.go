package nativeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

type songDeleteResponse struct {
	Status      string `json:"status"`
	ID          string `json:"id"`
	TrashedPath string `json:"trashedPath"`
}

func (api *Router) addSongDeleteRoute(r chi.Router) {
	r.Delete("/song/{id}", deleteSong(api.ds, api.maintenance))
}

func deleteSong(ds model.DataStore, maintenance core.Maintenance) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		id := chi.URLParam(request, "id")

		// 1. Look up song by ID
		mediaFile, err := ds.MediaFile(ctx).Get(id)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				http.Error(responseWriter, "song not found", http.StatusNotFound)
				return
			}
			log.Error(ctx, "Error looking up media file", "id", id, err)
			http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
			return
		}

		// 2. Determine trash directory
		trashDir := conf.Server.TrashDirectory
		if trashDir == "" {
			// Default: sibling of the first library's music folder
			trashDir = filepath.Join(conf.Server.MusicFolder, "..", ".navidrome-trash")
		}
		trashDir = filepath.Clean(trashDir)

		// 3. Validate trash directory is outside all library paths
		absFilePath := mediaFile.AbsolutePath()
		cleanLibPath := filepath.Clean(mediaFile.LibraryPath)
		cleanTrashDir := filepath.Clean(trashDir)
		if cleanLibPath != "" && cleanLibPath != "." &&
			(strings.HasPrefix(cleanTrashDir, cleanLibPath+string(os.PathSeparator)) || cleanTrashDir == cleanLibPath) {
			log.Error(ctx, "Trash directory is inside library path", "trashDir", trashDir, "libraryPath", mediaFile.LibraryPath)
			http.Error(responseWriter, "trash directory must be outside library path", http.StatusInternalServerError)
			return
		}

		// 4. Create trash directory if needed
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			log.Error(ctx, "Error creating trash directory", "trashDir", trashDir, err)
			http.Error(responseWriter, "failed to create trash directory", http.StatusInternalServerError)
			return
		}

		// 5. Move file to trash (or skip if already missing from disk)
		trashedPath := ""
		if _, statErr := os.Stat(absFilePath); statErr == nil {
			// File exists — move to trash with timestamp prefix
			originalFilename := filepath.Base(absFilePath)
			timestamp := time.Now().Format("20060102T150405")
			trashFilename := fmt.Sprintf("%s_%s", timestamp, originalFilename)
			trashedPath = filepath.Join(trashDir, trashFilename)

			if err := moveFile(absFilePath, trashedPath); err != nil {
				log.Error(ctx, "Error moving file to trash", "from", absFilePath, "to", trashedPath, err)
				http.Error(responseWriter, "failed to move file to trash", http.StatusInternalServerError)
				return
			}
			log.Info(ctx, "Moved file to trash", "id", id, "from", absFilePath, "to", trashedPath)
		} else if os.IsNotExist(statErr) {
			// File already missing from disk — just purge the DB record
			log.Info(ctx, "File already missing from disk, purging DB record", "id", id, "path", absFilePath)
			trashedPath = "(file was already missing)"
		} else {
			log.Error(ctx, "Error checking file existence", "path", absFilePath, statErr)
			http.Error(responseWriter, "failed to check file status", http.StatusInternalServerError)
			return
		}

		// 6. Mark as missing in DB, then purge via maintenance (handles GC + stat refresh)
		if maintenance == nil {
			log.Error(ctx, "Maintenance service not configured")
			http.Error(responseWriter, "maintenance service not available", http.StatusInternalServerError)
			return
		}
		// Mark the record as missing first — DeleteMissingFiles requires missing=true
		if err := ds.MediaFile(ctx).MarkMissing(true, mediaFile); err != nil {
			log.Error(ctx, "Error marking media file as missing", "id", id, err)
			http.Error(responseWriter, "file moved to trash but failed to mark as missing", http.StatusInternalServerError)
			return
		}
		if err := maintenance.DeleteMissingFiles(ctx, []string{id}); err != nil {
			log.Error(ctx, "Error purging DB record", "id", id, err)
			http.Error(responseWriter, "file moved to trash but DB purge failed", http.StatusInternalServerError)
			return
		}
		log.Info(ctx, "Purged DB record and ran GC", "id", id)

		// 7. Return success
		resp := songDeleteResponse{
			Status:      "deleted",
			ID:          id,
			TrashedPath: trashedPath,
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(responseWriter).Encode(resp)
	}
}

// moveFile moves a file from src to dst. Falls back to copy+delete if os.Rename
// fails (e.g. cross-device moves where src and dst are on different filesystems).
func moveFile(sourcePath, destPath string) error {
	if err := os.Rename(sourcePath, destPath); err == nil {
		return nil
	}
	// Fallback: copy then delete (handles cross-device / cross-volume moves)
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		os.Remove(destPath) // clean up partial copy
		return fmt.Errorf("copy: %w", err)
	}
	if err := destFile.Close(); err != nil {
		return fmt.Errorf("close dest: %w", err)
	}
	return os.Remove(sourcePath)
}
