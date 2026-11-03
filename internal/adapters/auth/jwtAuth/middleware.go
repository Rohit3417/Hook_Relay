package jwtauth

import (
	"context"
	"crypto/rsa"
	"net/http"
	"strings"
)

// contextKey is a custom type to prevent context key collisions in Go
type contextKey string

const (
	UserIDKey   contextKey = "userID"
	TenantIDKey contextKey = "tenantID"
)

// AuthMiddleware intercepts requests to validate the RS256 JWT
func AuthMiddleware(publicKey *rsa.PublicKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Extract the Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			// 2. Validate the "Bearer <token>" format
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, "invalid authorization format", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]

			// 3. Verify the token using your strictly enforced RS256 function
			claims, err := VerifyToken(tokenString, publicKey)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			// 4. Safely extract the claims
			userID, _ := claims["sub"].(string)
			tenantID, _ := claims["tenant_id"].(string)

			// 5. Inject the identities into the request context
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, TenantIDKey, tenantID)

			// 6. Pass the enriched context to the next handler
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
