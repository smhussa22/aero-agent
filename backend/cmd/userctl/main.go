// Command userctl creates a dashboard user. The password is read from stdin so it never
// appears in shell history or the process list.
//
//	echo 'a long passphrase' | userctl create <email> <admin|viewer> [--org <org_id> | --new-org "<name>"]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func main() {
	usage := func() {
		fmt.Fprintln(os.Stderr, `usage: userctl create <email> <admin|viewer> [--org <org_id> | --new-org "<name>"]   (password on stdin)`)
		os.Exit(2)
	}
	if len(os.Args) < 4 || os.Args[1] != "create" || (os.Args[3] != "admin" && os.Args[3] != "viewer") {
		usage()
	}
	email, role := os.Args[2], os.Args[3]
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	org := fs.String("org", "", "organization id (optional when there is exactly one)")
	newOrg := fs.String("new-org", "", "create an organization with this name and add the user to it")
	fs.Parse(os.Args[4:])
	if *org != "" && *newOrg != "" {
		usage()
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

	switch {
	case *newOrg != "":
		if *org, err = store.CreateOrg(ctx, pool, *newOrg); err != nil {
			slog.Error("create organization failed", "err", err)
			os.Exit(1)
		}
	case *org == "":
		if *org, err = store.SoleOrg(ctx, pool); err != nil {
			slog.Error("which organization?", "err", err)
			os.Exit(1)
		}
	}

	if err := store.CreateUser(ctx, pool, *org, email, password, role); err != nil {
		slog.Error("create user failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("created %s (%s) in organization %s\n", email, role, *org)
}
