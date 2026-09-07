package main

import (
	"Hook_Relay2/internal/adapters/handler"
	"Hook_Relay2/internal/adapters/redisqueue"
	"Hook_Relay2/internal/adapters/storage"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No .env found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := storage.NewPool(ctx)
	if err != nil {
		log.Fatalf("%v", err)
		return
	}
	defer pool.Close()

	client, err := redisqueue.NewRedisConnection(ctx)
	if err != nil {
		log.Fatalf("%v", err)
		return
	}
	defer client.Close()

	queue := redisqueue.NewClient(client)
	if err := queue.EnsureGroup(ctx, "dispatcher_group"); err != nil {
		log.Fatalf("%v", err)
	}

	h := handler.NewHandler(pool, client, queue)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Route("/", func(r chi.Router) {
		r.Get("/healthz", h.Healthz)
		r.Post("/api/v1/publish", h.Publish)
	})

	port := os.Getenv("PORT")
	fmt.Printf("Listening on port : %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		fmt.Printf("server error")
	}
}
