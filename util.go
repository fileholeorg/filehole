package main

import (
	"net/http"
)

func NoDirectoryList(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" {
			http.Error(w, "404 page not found", 404)
			return
		}
		h.ServeHTTP(w, r)
	})
}
