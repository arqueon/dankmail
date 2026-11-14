package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/arqueon/dux-mail/core/config"
	"github.com/arqueon/dux-mail/core/ent"
	entaccount "github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/internal/accounts"
	"github.com/arqueon/dux-mail/core/internal/i18n"
	"github.com/arqueon/dux-mail/core/internal/keyring"
	"github.com/arqueon/dux-mail/core/internal/oauth"
	"github.com/arqueon/dux-mail/core/internal/paths"
	"github.com/arqueon/dux-mail/core/repo"
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

	addMicrosoft := &cobra.Command{
		Use:   "add-microsoft",
		Short: "Add an Outlook.com / Microsoft 365 account (opens the browser for OAuth consent; the address is read from the authorized profile)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			clientID, _ := c.Flags().GetString("client-id")
			return runAccountAddMicrosoft(clientID)
		},
	}
	addMicrosoft.Flags().String("client-id", "", "Application (client) ID of your Azure app registration (public client)")

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
	addIMAP.Flags().String("smtp-host", "", "SMTP host for sending")
	addIMAP.Flags().Int("smtp-port", 0, "SMTP port (default 587)")
	addIMAP.Flags().String("webmail", "", "webmail URL for deep links")

	// dcal-parity subcommand groups: `dmail account setup google|microsoft`
	// and `dmail account add google|microsoft|imap`.
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Store reusable OAuth desktop client credentials",
	}
	setupGoogle := &cobra.Command{
		Use:     "google",
		Aliases: []string{"gmail"},
		Short:   "Save a reusable Google OAuth desktop client (from client_secret_*.json, stdin, or flags)",
		Args:    cobra.NoArgs,
		RunE:    runAccountSetupGoogle,
	}
	setupGoogle.Flags().String("client-json", "", "path to client_secret_*.json downloaded from Google Console")
	setupGoogle.Flags().Bool("stdin-json", false, "read client JSON from standard input")
	setupGoogle.Flags().String("client-id", "", "OAuth desktop client ID")
	setupGoogle.Flags().String("client-secret", "", "OAuth desktop client secret")

	setupMicrosoft := &cobra.Command{
		Use:   "microsoft",
		Short: "Save a reusable Microsoft Azure public-client Application ID",
		Args:  cobra.NoArgs,
		RunE:  runAccountSetupMicrosoft,
	}
	setupMicrosoft.Flags().String("client-id", "", "Application (client) ID of your Azure public-client registration")
	setup.AddCommand(setupGoogle, setupMicrosoft)

	addGroup := &cobra.Command{
		Use:   "add",
		Short: "Add a mail account (google, microsoft, or imap)",
	}
	addGroupGoogle := &cobra.Command{
		Use:     "google",
		Aliases: []string{"gmail"},
		Short:   addGmail.Short,
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			jsonPath, _ := c.Flags().GetString("client-json")
			return runAccountAddGmail(jsonPath)
		},
	}
	addGroupGoogle.Flags().String("client-json", "", "optional path to client_secret_*.json (uses saved default when omitted)")

	addGroupMicrosoft := &cobra.Command{
		Use:   "microsoft",
		Short: addMicrosoft.Short,
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			clientID, _ := c.Flags().GetString("client-id")
			return runAccountAddMicrosoft(clientID)
		},
	}
	addGroupMicrosoft.Flags().String("client-id", "", "optional Azure Application (client) ID (uses saved default when omitted)")

	addGroupIMAP := &cobra.Command{
		Use:   "imap <email>",
		Short: addIMAP.Short,
		Args:  cobra.ExactArgs(1),
		RunE:  func(c *cobra.Command, args []string) error { return runAccountAddIMAP(c, args[0]) },
	}
	addGroupIMAP.Flags().AddFlagSet(addIMAP.Flags())
	addGroup.AddCommand(addGroupGoogle, addGroupMicrosoft, addGroupIMAP)

	cmd.AddCommand(setup, addGroup, addGmail, addMicrosoft, addIMAP, list, remove, reauth)
	return cmd
}

func runAccountSetupGoogle(c *cobra.Command, _ []string) error {
	jsonPath, _ := c.Flags().GetString("client-json")
	stdinJSON, _ := c.Flags().GetBool("stdin-json")
	clientID, _ := c.Flags().GetString("client-id")
	clientSecret, _ := c.Flags().GetString("client-secret")

	var creds oauth.ClientCreds
	switch {
	case stdinJSON:
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		parsed, err := oauth.ParseClientJSON(raw)
		if err != nil {
			return err
		}
		creds = parsed
	case jsonPath != "":
		raw, err := readUserFile(jsonPath)
		if err != nil {
			return err
		}
		parsed, err := oauth.ParseClientJSON(raw)
		if err != nil {
			return err
		}
		creds = parsed
	case strings.TrimSpace(clientID) != "" && strings.TrimSpace(clientSecret) != "":
		creds = oauth.ClientCreds{
			ClientID:     strings.TrimSpace(clientID),
			ClientSecret: strings.TrimSpace(clientSecret),
		}
	default:
		return fmt.Errorf("pass --client-json <file>, --stdin-json, or --client-id + --client-secret")
	}

	if err := oauth.SaveDefaultClientCreds("google", creds); err != nil {
		return err
	}
	fmt.Println(i18n.T("Google OAuth client saved."))
	notifyDaemonReload()
	return nil
}

func runAccountSetupMicrosoft(c *cobra.Command, _ []string) error {
	clientID, _ := c.Flags().GetString("client-id")
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("pass --client-id <Application (client) ID from Azure>")
	}
	if err := oauth.SaveDefaultClientCreds("microsoft", oauth.ClientCreds{ClientID: clientID}); err != nil {
		return err
	}
	fmt.Println(i18n.T("Microsoft OAuth client saved."))
	notifyDaemonReload()
	return nil
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
					fmt.Println(i18n.T("Note:"), p.Note)
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

	fmt.Print(i18n.T("Password (or app password): "))
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}

	return withDB(func(ctx context.Context, db *ent.Client) error {
		fmt.Println(i18n.T("Testing the IMAP connection…"))
		res, err := accounts.AddIMAP(ctx, db, cfg, email, string(pw))
		if err != nil {
			return err
		}
		if res.Created {
			fmt.Printf(i18n.T("Account %s added (%s).\n"), res.Email, res.AccountID)
		} else {
			fmt.Printf(i18n.T("Account %s updated (%s).\n"), res.Email, res.AccountID)
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
	keyring.SetFallbackDB(db)
	return fn(ctx, db)
}

func runAccountAddMicrosoft(clientID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return withDB(func(ctx context.Context, db *ent.Client) error {
		accts, _ := db.Account.Query().Where(entaccount.TypeEQ(entaccount.TypeMicrosoft)).All(ctx)
		creds, err := oauth.ResolveMicrosoftClient(clientID, cfg.MicrosoftClientID, accountIDStringsByType(accts, entaccount.TypeMicrosoft)...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(clientID) != "" {
			_ = oauth.SaveDefaultClientCreds("microsoft", creds)
		}
		fmt.Println(i18n.T("Opening the browser to authorize the account…"))
		broker := oauth.NewBrokerFor(oauth.MicrosoftEndpoints, creds.ClientID, "", cfg.OAuthBindAddr)
		tok, err := broker.Authorize(ctx)
		if err != nil {
			return err
		}
		res, err := accounts.FinishMicrosoft(ctx, db, creds, tok)
		if err != nil {
			return err
		}
		if res.Created {
			fmt.Printf(i18n.T("Account %s added (%s).\n"), res.Email, res.AccountID)
		} else {
			fmt.Printf(i18n.T("Account %s re-authorized (%s).\n"), res.Email, res.AccountID)
		}
		notifyDaemonReload()
		return nil
	})
}

func runAccountAddGmail(clientJSONPath string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var explicit oauth.ClientCreds
	if clientJSONPath != "" {
		raw, err := readUserFile(clientJSONPath)
		if err != nil {
			return err
		}
		explicit, err = oauth.ParseClientJSON(raw)
		if err != nil {
			return err
		}
	}

	return withDB(func(ctx context.Context, db *ent.Client) error {
		accts, _ := db.Account.Query().Where(entaccount.TypeEQ(entaccount.TypeGmail)).All(ctx)
		creds, err := oauth.ResolveGoogleClient(
			explicit,
			cfg.GoogleClientID,
			cfg.GoogleClientSecret,
			accountIDStringsByType(accts, entaccount.TypeGmail)...,
		)
		if err != nil {
			return err
		}
		if explicit.ClientID != "" && explicit.ClientSecret != "" {
			_ = oauth.SaveDefaultClientCreds("google", creds)
		}
		fmt.Println(i18n.T("Opening the browser to authorize the account…"))
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
			fmt.Printf(i18n.T("Account %s added (%s).\n"), res.Email, res.AccountID)
		} else {
			fmt.Printf(i18n.T("Account %s re-authorized (%s).\n"), res.Email, res.AccountID)
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
		fmt.Println(i18n.T("Account removed (the remote mailbox is untouched)."))
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
		acct, err := db.Account.Get(ctx, uid)
		if err != nil {
			return err
		}
		explicit, _ := oauth.LoadClientCreds(id)
		allAccts, _ := db.Account.Query().All(ctx)
		var creds oauth.ClientCreds
		var broker *oauth.Broker
		switch acct.Type {
		case entaccount.TypeMicrosoft:
			creds, err = oauth.ResolveMicrosoftClient(explicit.ClientID, cfg.MicrosoftClientID, accountIDStringsByType(allAccts, entaccount.TypeMicrosoft)...)
			if err != nil {
				return err
			}
			broker = oauth.NewBrokerFor(oauth.MicrosoftEndpoints, creds.ClientID, "", cfg.OAuthBindAddr)
		case entaccount.TypeGmail:
			creds, err = oauth.ResolveGoogleClient(explicit, cfg.GoogleClientID, cfg.GoogleClientSecret, accountIDStringsByType(allAccts, entaccount.TypeGmail)...)
			if err != nil {
				return err
			}
			broker = oauth.NewBroker(creds.ClientID, creds.ClientSecret, cfg.OAuthBindAddr)
		default:
			return fmt.Errorf("re-authentication applies to OAuth accounts only; re-add the account to update an IMAP password")
		}
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
		fmt.Println(i18n.T("Account re-authenticated."))
		notifyDaemonReload()
		return nil
	})
}

// notifyDaemonReload pokes a running daemon; silence if there is none.
func notifyDaemonReload() {
	if err := callSimple("system.reload", nil); err == nil {
		fmt.Println(i18n.T("Daemon reloaded."))
	} else {
		fmt.Println(i18n.T("(Daemon not running; changes apply when it starts.)"))
	}
}
