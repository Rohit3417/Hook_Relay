package main

import (
	jwtauth "Hook_Relay2/internal/adapters/auth/jwtAuth"
	"Hook_Relay2/internal/adapters/graphql"
	"Hook_Relay2/internal/adapters/graphql/graph"
	"Hook_Relay2/internal/adapters/handler"
	"Hook_Relay2/internal/adapters/redisqueue"
	"Hook_Relay2/internal/adapters/storage"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/playground"
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

	// 1. Load RSA Private Key for signing JWTs
	privBytes, err := os.ReadFile(os.Getenv("JWT_PRIVATE_KEY_PATH"))
	if err != nil {
		log.Fatalf("failed to read private.pem: %v", err)
	}
	privKey, err := jwtauth.LoadPrivateKey(privBytes)
	if err != nil {
		log.Fatalf("failed to parse private.pem: %v", err)
	}

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

	resolver := &graph.Resolver{Pool: pool, Client: client}
	gqlHandler := graphql.NewHandler(pool, resolver)

	h := handler.NewHandler(pool, client, queue, privKey)
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	// RS256 JWT Authentication middleware for GraphQL
	var authMiddleware func(http.Handler) http.Handler
	pubKeyPath := os.Getenv("JWT_PUBLIC_KEY_PATH")
	if pubKeyPath != "" {
		if pubBytes, err := os.ReadFile(pubKeyPath); err == nil {
			if pubKey, err := jwtauth.LoadPublicKey(pubBytes); err == nil {
				authMiddleware = jwtauth.AuthMiddleware(pubKey)
			} else {
				log.Printf("failed to parse JWT public key from path: %v", err)
			}
		} else {
			log.Printf("failed to read JWT public key file: %v", err)
		}
	} else if pubKeyEnv := os.Getenv("JWT_PUBLIC_KEY"); pubKeyEnv != "" {
		if pubKey, err := jwtauth.LoadPublicKey([]byte(pubKeyEnv)); err == nil {
			authMiddleware = jwtauth.AuthMiddleware(pubKey)
		} else {
			log.Printf("failed to parse JWT public key from env: %v", err)
		}
	}

	if authMiddleware != nil {
		r.Use(authMiddleware)
	}

	//REST routes
	r.Route("/", func(r chi.Router) {
		r.Get("/healthz", h.Healthz)
		r.Post("/api/v1/publish", h.Publish)
	})

	//  Mount auth routes in Chi router
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/signup", h.Signup)
		r.Post("/login", h.Login)
	})

	//Graphql	Routes
	r.Handle("/query", gqlHandler)
	r.Handle("/", playground.Handler("GraphQL playground", "/query"))

	port := os.Getenv("PORT")
	fmt.Printf("Listening on port : %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		fmt.Printf("server error")
	}
}
