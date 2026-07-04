package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arqueon/dankmail/core/config"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/internal/keyring"
	"github.com/arqueon/dankmail/core/internal/oauth"
	"github.com/arqueon/dankmail/core/internal/paths"
	"github.com/arqueon/dankmail/core/repo"
)

// accountCmd manages accounts directly against the database + keyring.
// The daemon picks changes up via system.reload (sent automatically when
// it is running). SQLite in WAL mode tolerates this second writer.
func accountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage mail accounts"}

	addGmail := &cobra.Command{
		Use:   "add-gmail <email>",
		Short: "Add a Gmail account (opens the browser for OAuth consent)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(c *cobra.Command, args []string) error { return runAccountAddGmail(args[0]) },
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List configured accounts",
		RunE: func(c *cobra.Command, args []string) error {
			return withDB(func(ctx context.Context, db *ent.Client) error {
				accounts, err := repo.New(db).Accounts(ctx)
				if err != nil {
					return err
				}
				for _, a := range accounts {
					fmt.Printf("%s  %-6s %-30s %s\n", a.ID, a.Type, a.Email, a.Status)
				}
				return nil
			})
		},
	}

	remove := &cobra.Command{
		Use:   "remove <account-id>",
		Short: "Remove an account (local cache + keyring secrets; the remote mailbox is untouched)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(c *cobra.Command, args []string) error { return runAccountRemove(args[0]) },
	}

	reauth := &cobra.Command{
		Use:   "reauth <account-id>",
		Short: "Re-run the OAuth consent for an account",
		Args:  cobra.ExactArgs(1),
		RunE:  func(c *cobra.Command, args []string) error { return runAccountReauth(args[0]) },
	}

	cmd.AddCommand(addGmail, list, remove, reauth)
	return cmd
}

func withDB(fn func(ctx context.Context, db *ent.Client) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	dbPath := cfg.DatabasePath
	if dbPath == "" {
		dbPath = paths.DatabasePath()
	}
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(ctx, db)
}

func runAccountAddGmail(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.GoogleClientID == "" || cfg.GoogleClientSecret == "" {
		return fmt.Errorf("set DMAIL_GOOGLE_CLIENT_ID and DMAIL_GOOGLE_CLIENT_SECRET first (see docs/gmail-setup.md)")
	}

	return withDB(func(ctx context.Context, db *ent.Client) error {
		acct, err := db.Account.Create().
			SetType("gmail").
			SetEmail(email).
			Save(ctx)
		if err != nil {
			return err
		}

		fmt.Println("Abriendo el navegador para autorizar la cuenta…")
		broker := oauth.NewBroker(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.OAuthBindAddr)
		tok, err := broker.Authorize(ctx)
		if err != nil {
			_ = db.Account.DeleteOne(acct).Exec(ctx)
			return err
		}
		if err := oauth.SaveToken(acct.ID.String(), tok); err != nil {
			_ = db.Account.DeleteOne(acct).Exec(ctx)
			return err
		}

		fmt.Printf("Cuenta %s añadida (%s).\n", email, acct.ID)
		notifyDaemonReload()
		return nil
	})
}

func runAccountRemove(id string) error {
	return withDB(func(ctx context.Context, db *ent.Client) error {
		uid, err := parseUUID(id)
		if err != nil {
			return fmt.Errorf("bad account id: %w", err)
		}
		if err := db.Account.DeleteOneID(uid).Exec(ctx); err != nil {
			return err
		}
		for _, key := range []string{keyring.KeyOAuthToken, keyring.KeyIMAPPassword, keyring.KeySMTPPassword} {
			_ = keyring.Delete(id, key)
		}
		fmt.Println("Cuenta eliminada (la bandeja remota queda intacta).")
		notifyDaemonReload()
		return nil
	})
}

func runAccountReauth(id string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return withDB(func(ctx context.Context, db *ent.Client) error {
		uid, err := parseUUID(id)
		if err != nil {
			return fmt.Errorf("bad account id: %w", err)
		}
		broker := oauth.NewBroker(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.OAuthBindAddr)
		tok, err := broker.Authorize(ctx)
		if err != nil {
			return err
		}
		if err := oauth.SaveToken(id, tok); err != nil {
			return err
		}
		_, err = db.Account.UpdateOneID(uid).
			SetStatus("active").
			SetLastError("").
			Save(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Cuenta re-autenticada.")
		notifyDaemonReload()
		return nil
	})
}

// notifyDaemonReload pokes a running daemon; silence if there is none.
func notifyDaemonReload() {
	if err := callSimple("system.reload", nil); err == nil {
		fmt.Println("Daemon recargado.")
	} else {
		fmt.Println("(Daemon no activo; los cambios aplican al arrancarlo.)")
	}
}
