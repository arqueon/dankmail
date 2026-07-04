package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arqueon/dankmail/core/config"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/internal/accounts"
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
		Use:   "add-gmail",
		Short: "Add a Gmail account (opens the browser for OAuth consent; the address is read from the authorized profile)",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, args []string) error { return runAccountAddGmail() },
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

func runAccountAddGmail() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.GoogleClientID == "" || cfg.GoogleClientSecret == "" {
		return fmt.Errorf("set DMAIL_GOOGLE_CLIENT_ID and DMAIL_GOOGLE_CLIENT_SECRET first,\nor use the in-app wizard (tray → Open Dank Mail → add account), which walks\nyou through creating the OAuth client (see also docs/gmail-setup.md)")
	}
	creds := oauth.ClientCreds{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret}

	return withDB(func(ctx context.Context, db *ent.Client) error {
		fmt.Println("Abriendo el navegador para autorizar la cuenta…")
		broker := oauth.NewBroker(creds.ClientID, creds.ClientSecret, cfg.OAuthBindAddr)
		tok, err := broker.Authorize(ctx)
		if err != nil {
			return err
		}
		res, err := accounts.FinishGmail(ctx, db, creds, tok)
		if err != nil {
			return err
		}
		if res.Created {
			fmt.Printf("Cuenta %s añadida (%s).\n", res.Email, res.AccountID)
		} else {
			fmt.Printf("Cuenta %s re-autorizada (%s).\n", res.Email, res.AccountID)
		}
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
		for _, key := range []string{keyring.KeyOAuthToken, keyring.KeyOAuthClient, keyring.KeyIMAPPassword, keyring.KeySMTPPassword} {
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
		// Prefer the OAuth client stored with the account at add time.
		creds, err := oauth.LoadClientCreds(id)
		if err != nil || creds.ClientID == "" {
			creds = oauth.ClientCreds{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret}
		}
		if creds.ClientID == "" || creds.ClientSecret == "" {
			return fmt.Errorf("no OAuth client for this account; set DMAIL_GOOGLE_CLIENT_ID/SECRET or re-add via the wizard")
		}
		broker := oauth.NewBroker(creds.ClientID, creds.ClientSecret, cfg.OAuthBindAddr)
		tok, err := broker.Authorize(ctx)
		if err != nil {
			return err
		}
		if err := oauth.SaveToken(id, tok); err != nil {
			return err
		}
		if err := oauth.SaveClientCreds(id, creds); err != nil {
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
