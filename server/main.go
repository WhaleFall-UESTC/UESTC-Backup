package main

import (
	"log"
	"net/http"
)

func newRouter(s *Store) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	return mux
}

func main() {
	store, err := NewStore("./data")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", newRouter(store)))
}
