// Command hostctl registers a host and prints its ingest token (shown once).
// Running it again for the same host rotates the token.
//
//	hostctl add <host_id>
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "add" || os.Args[2] == "" {
		fmt.Fprintln(os.Stderr, "usage: hostctl add <host_id>")
		os.Exit(2)
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://aero:aero@localhost:5432/aero"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		slog.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	token, err := store.IssueToken(ctx, pool, os.Args[2])
	if err != nil {
		slog.Error("issue token failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("host:  %s\ntoken: %s\n(store it now; it cannot be shown again)\n", os.Args[2], token)
}
