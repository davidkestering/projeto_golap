package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// uiFiles embute a SPA estática (drag-and-drop) servida em /ui/.
//
//go:embed ui/*
var uiFiles embed.FS

// registerUI monta a SPA em /ui/ a partir dos arquivos embutidos.
func registerUI(mux *http.ServeMux) {
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err) // erro de embed = bug de build
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", fileServer))
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
}
