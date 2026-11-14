package main

import (
	jwtauth "Hook_Relay2/internal/adapters/auth/jwtAuth"
	"Hook_Relay2/internal/adapters/graphql"
	"Hook_Relay2/internal/adapters/graphql/graph"
	"Hook_Relay2/internal/adapters/handler"
	"Hook_Relay2/internal/adapters/redisqueue"
	"Hook_Relay2/internal/adapters/storage"
	"context"
	"crypto/rsa"
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
	totpKey := []byte(os.Getenv("TOTP_MASTER_KEY"))

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

	h := handler.NewHandler(pool, client, queue, privKey, totpKey)
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	// RS256 JWT Authentication middleware for GraphQL
	authMiddleware := loadAuthMiddleware()

	// 1. Inbound Webhook (Public, no JWT — verified via Razorpay HMAC signature)
	r.Post("/api/v1/billing/razorpay-webhook", h.HandleRazorpayWebhook)

	// 2. Create Order (Protected, requires JWT)
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/api/v1/billing/create-order", h.CreateRazorpayOrder)
		r.Handle("/query", gqlHandler)
	})
	// 1. PUBLIC ROUTES (No JWT required)
	r.Get("/healthz", h.Healthz)

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/signup", h.Signup)
		r.Post("/login", h.Login)
	})

	r.Post("/api/v1/publish", h.Publish) // Uses its own API key, not JWT

	r.Handle("/", playground.Handler("GraphQL playground", "/query"))

	port := os.Getenv("PORT")
	fmt.Printf("Listening on port : %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		fmt.Printf("server error")
	}
}

// loadAuthMiddleware returns the JWT middleware. It exits at startup if the
// public key is missing or invalid, so protected routes can never run unauthenticated.
func loadAuthMiddleware() func(http.Handler) http.Handler {
	pubKeyPath := os.Getenv("JWT_PUBLIC_KEY_PATH")
	if pubKeyPath == "" {
		log.Fatalf("JWT_PUBLIC_KEY_PATH is not set")
	}
	pubBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		log.Fatalf("failed to read JWT public key file: %v", err)
	}
	var pubKey *rsa.PublicKey
	pubKey, err = jwtauth.LoadPublicKey(pubBytes)
	if err != nil {
		log.Fatalf("failed to parse JWT public key: %v", err)
	}
	return jwtauth.AuthMiddleware(pubKey)
}
