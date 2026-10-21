package graphql

import (
	"Hook_Relay2/internal/adapters/graphql/dataloaders"
	"Hook_Relay2/internal/adapters/graphql/graph"
	"context"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/graph-gophers/dataloader/v7"
	"github.com/jackc/pgx/v5/pgxpool"
)

//const defaultPort = "8080"

// 2. The Middleware that injects the loaders into the request context
func DataloaderMiddleware(pool *pgxpool.Pool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Initialize the specific batch function and loader
		statsBatchFn := dataloaders.GetStatsBatch(pool)
		statsLoader := dataloader.NewBatchedLoader(statsBatchFn)

		// Group them
		loaders := &dataloaders.Loaders{
			StatsLoader: statsLoader,
		}

		// Inject into context
		ctx := context.WithValue(r.Context(), dataloaders.LoadersKey, loaders)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// 3. A helper to expose the fully wrapped handler to main.go
func NewHandler(pool *pgxpool.Pool, resolver *graph.Resolver) http.Handler {
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	// Wrap the GraphQL server with the DataLoader middleware
	return DataloaderMiddleware(pool, srv)
}

// func main() {
// 	port := os.Getenv("PORT")
// 	if port == "" {
// 		port = defaultPort
// 	}

// 	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{}}))

// 	srv.AddTransport(transport.Options{})
// 	srv.AddTransport(transport.GET{})
// 	srv.AddTransport(transport.POST{})

// 	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

// 	srv.Use(extension.Introspection{})
// 	srv.Use(extension.AutomaticPersistedQuery{
// 		Cache: lru.New[string](100),
// 	})

// 	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
// 	http.Handle("/query", srv)

// 	log.Printf("connect to http://localhost:%s/ for GraphQL playground", port)
// 	log.Fatal(http.ListenAndServe(":"+port, nil))
// }
