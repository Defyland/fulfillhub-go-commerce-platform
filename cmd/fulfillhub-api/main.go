package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/api"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	service := commerce.NewService(commerce.NewMemoryStore())
	server := api.NewServer(service)

	log.Printf("starting fulfillhub api on %s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatal(err)
	}
}
