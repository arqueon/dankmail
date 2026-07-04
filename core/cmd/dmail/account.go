package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
		RunE: func(c *cobra.Command, args []string) error {
			jsonPath, _ := c.Flags().GetString("client-json")
			return runAccountAddGmail(jsonPath)
		},
	}
	addGmail.Flags().String("client-json", "", "path to the client_secret_*.json downloaded from Google Console")

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

	addIMAP := &cobra.Command{
		Use:   "add-imap <email>",
		Short: "Add a generic IMAP account (password read from stdin; connection tested before saving)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(c *cobra.Command, args []string) error { return runAccountAddIMAP(c, args[0]) },
	}
	addIMAP.Flags().String("preset", "", "provider preset: icloud|outlook|yahoo|fastmail|proton")
	addIMAP.Flags().String("host", "", "IMAP host (overrides preset)")
	addIMAP.Flags().Int("port", 0, "IMAP port (default 993)")
	addIMAP.Flags().String("security", "", "tls|starttls (default tls)")
	addIMAP.Flags().String("username", "", "login user if different from the email")
	addIMAP.Flags().String("smtp-host", "", "SMTP host for sending (anillo 2)")
	addIMAP.Flags().Int("smtp-port", 0, "SMTP port (default 587)")
	addIMAP.Flags().String("webmail", "", "webmail URL for deep links")

	cmd.AddCommand(addGmail, addIMAP, list, remove, reauth)
	return cmd
}

func runAccountAddIMAP(c *cobra.Command, email string) error {
	cfg := accounts.IMAPConfig{}
	if presetKey, _ := c.Flags().GetString("preset"); presetKey != "" {
		found := false
		for _, p := range accounts.IMAPPresets() {
			if p.Key == presetKey {
				cfg.Host, cfg.Port, cfg.Security = p.Host, p.Port, p.Security
				cfg.SMTPHost, cfg.SMTPPort = p.SMTPHost, p.SMTPPort
				cfg.WebmailURL = p.WebmailURL
				if p.Note != "" {
					fmt.Println("Nota:", p.Note)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown preset %q", presetKey)
		}
	}
	if v, _ := c.Flags().GetString("host"); v != "" {
		cfg.Host = v
	}
	if v, _ := c.Flags().GetInt("port"); v != 0 {
		cfg.Port = v
	}
	if v, _ := c.Flags().GetString("security"); v != "" {
		cfg.Security = v
	}
	cfg.Username, _ = c.Flags().GetString("username")
	if v, _ := c.Flags().GetString("smtp-host"); v != "" {
		cfg.SMTPHost = v
	}
	if v, _ := c.Flags().GetInt("smtp-port"); v != 0 {
		cfg.SMTPPort = v
	}
	if v, _ := c.Flags().GetString("webmail"); v != "" {
		cfg.WebmailURL = v
	}

	fmt.Print("Contraseña (o app password): ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}

	return withDB(func(ctx context.Context, db *ent.Client) error {
		fmt.Println("Probando conexión IMAP…")
		res, err := accounts.AddIMAP(ctx, db, cfg, email, string(pw))
		if err != nil {
			return err
		}
		if res.Created {
			fmt.Printf("Cuenta %s añadida (%s). El sync IMAP llega en el anillo 2; queda en pausa.\n", res.Email, res.AccountID)
		} else {
			fmt.Printf("Cuenta %s actualizada (%s).\n", res.Email, res.AccountID)
		}
		notifyDaemonReload()
		return nil
	})
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

func runAccountAddGmail(clientJSONPath string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var creds oauth.ClientCreds
	if clientJSONPath != "" {
		raw, err := os.ReadFile(clientJSONPath)
		if err != nil {
			return err
		}
		creds, err = oauth.ParseClientJSON(raw)
		if err != nil {
			return err
		}
	} else if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		creds = oauth.ClientCreds{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret}
	} else {
		return fmt.Errorf("pass --client-json <client_secret_*.json>, set DMAIL_GOOGLE_CLIENT_ID/SECRET,\nor use the in-app wizard (tray → Open Dank Mail → add account); see docs/gmail-setup.md")
	}

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
