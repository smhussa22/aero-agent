// Command hostctl registers a server in an organization and prints its token (shown once).
// Running it again for the same name rotates the token.
//
//	hostctl add [--org <org_id>] <name>
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "add" {
		fmt.Fprintln(os.Stderr, "usage: hostctl add [--org <org_id>] <name>")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	org := fs.String("org", "", "organization id (optional when there is exactly one)")
	fs.Parse(os.Args[2:])
	if fs.NArg() != 1 || fs.Arg(0) == "" || len(fs.Arg(0)) > 100 {
		fmt.Fprintln(os.Stderr, "usage: hostctl add [--org <org_id>] <name>")
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

	if *org == "" {
		if *org, err = store.SoleOrg(ctx, pool); err != nil {
			slog.Error("which organization?", "err", err)
			os.Exit(1)
		}
	}
	id, token, err := store.IssueToken(ctx, pool, *org, fs.Arg(0))
	if err != nil {
		slog.Error("issue token failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("host:  %s (id %s)\ntoken: %s\n(store it now; it cannot be shown again)\n", fs.Arg(0), id, token)
}
