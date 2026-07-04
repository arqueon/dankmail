package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/arqueon/dankmail/core/ent"
	entaccount "github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/keyring"
)

// IMAPConfig is the non-secret connection config stored in
// Account.config. The password lives in the keyring only.
type IMAPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`     // default 993
	Security string `json:"security"` // "tls" (default) | "starttls"
	// Username defaults to the email address when empty.
	Username string `json:"username,omitempty"`
	SMTPHost string `json:"smtpHost,omitempty"`
	SMTPPort int    `json:"smtpPort,omitempty"` // default 587
	// WebmailURL opens the provider's web UI (mailbox-level deep link).
	WebmailURL string `json:"webmailUrl,omitempty"`
}

func (c *IMAPConfig) normalize(email string) error {
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return errors.New("IMAP host is required")
	}
	if c.Port == 0 {
		c.Port = 993
	}
	switch c.Security {
	case "", "tls":
		c.Security = "tls"
	case "starttls":
	default:
		return fmt.Errorf("unknown security %q (tls|starttls)", c.Security)
	}
	if c.Username == "" {
		c.Username = email
	}
	if c.SMTPPort == 0 && c.SMTPHost != "" {
		c.SMTPPort = 587
	}
	return nil
}

func (c *IMAPConfig) toMap() map[string]any {
	m := map[string]any{
		"host":     c.Host,
		"port":     float64(c.Port),
		"security": c.Security,
	}
	if c.Username != "" {
		m["username"] = c.Username
	}
	if c.SMTPHost != "" {
		m["smtpHost"] = c.SMTPHost
		m["smtpPort"] = float64(c.SMTPPort)
	}
	if c.WebmailURL != "" {
		m["webmailUrl"] = c.WebmailURL
	}
	return m
}

// TestIMAP dials the server and logs in — the add path runs this before
// anything is stored, so bad credentials fail fast with a clear error.
func TestIMAP(ctx context.Context, cfg IMAPConfig, email, password string) error {
	if err := cfg.normalize(email); err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	type dialResult struct {
		client *imapclient.Client
		err    error
	}
	ch := make(chan dialResult, 1)
	go func() {
		var (
			cli *imapclient.Client
			err error
		)
		if cfg.Security == "starttls" {
			cli, err = imapclient.DialStartTLS(addr, nil)
		} else {
			cli, err = imapclient.DialTLS(addr, nil)
		}
		ch <- dialResult{cli, err}
	}()

	var cli *imapclient.Client
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("cannot reach %s: %w", addr, res.err)
		}
		cli = res.client
	}
	defer cli.Close()

	if err := cli.Login(cfg.Username, password).Wait(); err != nil {
		return fmt.Errorf("login rejected for %s: %w", cfg.Username, err)
	}
	_ = cli.Logout().Wait()
	return nil
}

// AddIMAP validates the connection, then stores the account: config JSON
// in the row, password in the keyring. Re-adding the same address
// updates config + password and re-activates the account.
func AddIMAP(ctx context.Context, db *ent.Client, cfg IMAPConfig, email, password string) (Result, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return Result{}, errors.New("a valid email address is required")
	}
	if password == "" {
		return Result{}, errors.New("password (or app password) is required")
	}
	if err := cfg.normalize(email); err != nil {
		return Result{}, err
	}

	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := TestIMAP(testCtx, cfg, email, password); err != nil {
		return Result{}, err
	}

	acct, err := db.Account.Query().
		Where(entaccount.EmailEQ(email), entaccount.TypeEQ(entaccount.TypeImap)).
		Only(ctx)
	created := false
	switch {
	case ent.IsNotFound(err):
		acct, err = db.Account.Create().
			SetType(entaccount.TypeImap).
			SetEmail(email).
			SetConfig(cfg.toMap()).
			Save(ctx)
		if err != nil {
			return Result{}, err
		}
		created = true
	case err != nil:
		return Result{}, err
	default:
		if _, err := db.Account.UpdateOne(acct).
			SetConfig(cfg.toMap()).
			SetStatus(entaccount.StatusActive).
			SetLastError("").
			Save(ctx); err != nil {
			return Result{}, err
		}
	}

	if err := keyring.Set(acct.ID.String(), keyring.KeyIMAPPassword, password); err != nil {
		return Result{}, err
	}
	return Result{AccountID: acct.ID.String(), Email: email, Created: created}, nil
}
