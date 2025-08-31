package main

import (
	"log"
	"net/http"
)

func main() {
	fs := http.FileServer(http.Dir("testdata"))
	log.Println("Serving testdata/ on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", fs))
}
