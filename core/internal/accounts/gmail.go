package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/arqueon/dankmail/core/ent"
	entaccount "github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/oauth"
)

// Result of a completed account addition.
type Result struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	Created   bool   `json:"created"`
}

// FinishGmail stores a completed OAuth consent as an account: the email
// is fetched from the Gmail profile (no self-typed addresses), the token
// and the user's OAuth client go to the keyring, and an existing account
// for the same address is re-activated instead of duplicated.
func FinishGmail(ctx context.Context, db *ent.Client, creds oauth.ClientCreds, tok *oauth2.Token) (Result, error) {
	email, err := FetchGmailEmail(ctx, creds, tok)
	if err != nil {
		return Result{}, fmt.Errorf("fetch gmail profile: %w", err)
	}

	acct, err := db.Account.Query().
		Where(entaccount.EmailEQ(email), entaccount.TypeEQ(entaccount.TypeGmail)).
		Only(ctx)
	created := false
	switch {
	case ent.IsNotFound(err):
		acct, err = db.Account.Create().
			SetType(entaccount.TypeGmail).
			SetEmail(email).
			Save(ctx)
		if err != nil {
			return Result{}, err
		}
		created = true
	case err != nil:
		return Result{}, err
	default:
		if _, err := db.Account.UpdateOne(acct).
			SetStatus(entaccount.StatusActive).
			SetLastError("").
			Save(ctx); err != nil {
			return Result{}, err
		}
	}

	id := acct.ID.String()
	if err := oauth.SaveToken(id, tok); err != nil {
		return Result{}, err
	}
	if err := oauth.SaveClientCreds(id, creds); err != nil {
		return Result{}, err
	}
	return Result{AccountID: id, Email: email, Created: created}, nil
}

// FetchGmailEmail asks the Gmail API who the token belongs to. Works with
// the gmail.modify scope — no extra userinfo scope needed.
func FetchGmailEmail(ctx context.Context, creds oauth.ClientCreds, tok *oauth2.Token) (string, error) {
	cfg := &oauth2.Config{ClientID: creds.ClientID, ClientSecret: creds.ClientSecret}
	svc, err := gmailapi.NewService(ctx, option.WithHTTPClient(cfg.Client(ctx, tok)))
	if err != nil {
		return "", err
	}
	profile, err := svc.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	email := strings.ToLower(strings.TrimSpace(profile.EmailAddress))
	if email == "" {
		return "", errors.New("gmail profile did not return an email address")
	}
	return email, nil
}
