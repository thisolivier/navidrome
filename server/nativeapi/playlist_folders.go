package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

func (api *Router) addPlaylistFolderRoute(r chi.Router) {
	constructor := func(ctx context.Context) rest.Repository {
		return api.ds.PlaylistFolder(ctx)
	}

	r.Route("/playlist/folder", func(r chi.Router) {
		r.Get("/", rest.GetAll(constructor))
		r.Post("/", rest.Post(constructor))

		r.Route("/{id}", func(r chi.Router) {
			r.Use(server.URLParamsMiddleware)
			r.Get("/", getPlaylistFolder(api.ds))
			r.Put("/", rest.Put(constructor))
			r.Delete("/", rest.Delete(constructor))

			// Membership management
			r.Post("/playlist/{playlistId}", addPlaylistToFolder(api.ds))
			r.Delete("/playlist/{playlistId}", removePlaylistFromFolder(api.ds))
		})
	})
}

type playlistFolderDetailResponse struct {
	model.PlaylistFolder
	Children    model.PlaylistFolders `json:"children"`
	PlaylistIDs []string              `json:"playlistIds"`
}

func getPlaylistFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ctx := r.Context()

		repo := ds.PlaylistFolder(ctx)
		folder, err := repo.Get(id)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		children, err := repo.GetChildren(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		playlistIDs, err := repo.GetPlaylistIDs(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := playlistFolderDetailResponse{
			PlaylistFolder: *folder,
			Children:       children,
			PlaylistIDs:    playlistIDs,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func addPlaylistToFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := chi.URLParam(r, "id")
		playlistID := chi.URLParam(r, "playlistId")
		ctx := r.Context()

		repo := ds.PlaylistFolder(ctx)
		_, err := repo.Get(folderID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				http.Error(w, "folder not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = repo.AddPlaylist(folderID, playlistID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

func removePlaylistFromFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		folderID := chi.URLParam(r, "id")
		playlistID := chi.URLParam(r, "playlistId")
		ctx := r.Context()

		repo := ds.PlaylistFolder(ctx)
		_, err := repo.Get(folderID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				http.Error(w, "folder not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = repo.RemovePlaylist(folderID, playlistID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
