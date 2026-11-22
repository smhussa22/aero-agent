// Command userctl is for the operator. People sign in through your identity provider, so there are no passwords to
// set here; this command only lets someone in.
//
//	userctl invite <email> <admin|viewer> [--org <org_id> | --new-org "<name>"]
//
// With --new-org it creates an organization and invites its first admin. The person then signs in with that email
// address through the identity provider and lands in the organization. (Or set SIGNUP_OPEN=1 on the API and anyone
// who signs in gets a new organization of their own.)
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
	usage := func() {
		fmt.Fprintln(os.Stderr, `usage: userctl invite <email> <admin|viewer> [--org <org_id> | --new-org "<name>"]`)
		os.Exit(2)
	}
	if len(os.Args) < 4 || os.Args[1] != "invite" || (os.Args[3] != "admin" && os.Args[3] != "viewer") {
		usage()
	}
	email, role := os.Args[2], os.Args[3]
	fs := flag.NewFlagSet("invite", flag.ExitOnError)
	org := fs.String("org", "", "organization id (optional when there is exactly one)")
	newOrg := fs.String("new-org", "", "create an organization with this name and invite its first admin")
	fs.Parse(os.Args[4:])
	if *org != "" && *newOrg != "" {
		usage()
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

	switch {
	case *newOrg != "":
		if role != "admin" {
			fmt.Fprintln(os.Stderr, "the first person in a new organization must be an admin")
			os.Exit(2)
		}
		id, inv, err := store.CreateInviteForNewOrg(ctx, pool, *newOrg, email)
		if err != nil {
			slog.Error("could not create the organization and invitation", "err", err)
			os.Exit(1)
		}
		fmt.Printf("created organization %s and invited %s as admin (valid until %s)\n", id, inv.Email, inv.ExpiresAt.Format("2006-01-02"))
	default:
		if *org == "" {
			if *org, err = store.SoleOrg(ctx, pool); err != nil {
				slog.Error("which organization?", "err", err)
				os.Exit(1)
			}
		}
		inv, err := store.CreateInvite(ctx, pool, *org, "", email, role)
		if err != nil {
			slog.Error("could not create the invitation", "err", err)
			os.Exit(1)
		}
		fmt.Printf("invited %s as %s (valid until %s)\n", inv.Email, inv.Role, inv.ExpiresAt.Format("2006-01-02"))
	}
	fmt.Println("They sign in with that email address; nothing else to send them.")
}
