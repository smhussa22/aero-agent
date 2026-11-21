// Command userctl creates a web app user. The password is read from stdin so it never
// appears in shell history or the process list.
//
//	echo 'a long passphrase' | userctl create <email> <admin|viewer>
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func main() {
	if len(os.Args) != 4 || os.Args[1] != "create" || (os.Args[3] != "admin" && os.Args[3] != "viewer") {
		fmt.Fprintln(os.Stderr, "usage: userctl create <email> <admin|viewer>   (password on stdin)")
		os.Exit(2)
	}

	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && password == "" {
		fmt.Fprintln(os.Stderr, "no password on stdin")
		os.Exit(2)
	}
	password = strings.TrimRight(password, "\r\n")

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

	if err := store.CreateUser(ctx, pool, os.Args[2], password, os.Args[3]); err != nil {
		slog.Error("create user failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("created %s (%s)\n", os.Args[2], os.Args[3])
}
