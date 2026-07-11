package accounts

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/arqueon/dankmail/core/ent"
	entaccount "github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/oauth"
	"github.com/arqueon/dankmail/core/internal/provider/microsoft"
)

// FinishMicrosoft resolves the authorized mailbox address (Graph /me),
// upserts the account row, and stores token + client in the keyring —
// mirror of FinishGmail. Re-consenting an existing account reactivates
// it and clears its last error.
func FinishMicrosoft(ctx context.Context, db *ent.Client, creds oauth.ClientCreds, tok *oauth2.Token) (Result, error) {
	email, err := FetchGraphEmail(ctx, creds, tok)
	if err != nil {
		return Result{}, fmt.Errorf("fetch graph profile: %w", err)
	}

	acct, err := db.Account.Query().
		Where(entaccount.EmailEQ(email), entaccount.TypeEQ(entaccount.TypeMicrosoft)).
		Only(ctx)
	created := false
	switch {
	case ent.IsNotFound(err):
		acct, err = db.Account.Create().
			SetType(entaccount.TypeMicrosoft).
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

// FetchGraphEmail asks Microsoft Graph who the token belongs to.
func FetchGraphEmail(ctx context.Context, creds oauth.ClientCreds, tok *oauth2.Token) (string, error) {
	cfg := &oauth2.Config{ClientID: creds.ClientID}
	return microsoft.NewClient(cfg.Client(ctx, tok)).GetProfile(ctx)
}

// MicrosoftSetupSteps walks the user through creating their own Azure
// app registration (bring-your-own-client, same model as Gmail). Public
// client with PKCE: no secret is ever created or stored.
func MicrosoftSetupSteps() []SetupStep {
	return []SetupStep{
		{
			Title:       "Create an Azure app registration",
			Description: "On the App registrations page click \"New registration\". Name: anything (e.g. \"dankmail\"). Supported account types: \"Personal Microsoft accounts and work/school accounts\" — required for hotmail.com/outlook.com/live.com mailboxes. Personal accounts need no paid Azure subscription.",
			URL:         "https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
			URLLabel:    "Open App registrations",
		},
		{
			Title:       "Add the desktop platform",
			Description: "In your new app: Authentication → \"Add a platform\" → \"Mobile and desktop applications\" → custom redirect URI: http://localhost — then set \"Allow public client flows\" to Yes and save.",
			URL:         "https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
			URLLabel:    "Open App registrations",
			Note:        "No client secret is created: dankmail is a public client and the PKCE exchange carries the proof. The only mail permissions ever requested are Mail.ReadWrite and Mail.Send (plus User.Read for the address).",
		},
		{
			Title:       "Copy the Application (client) ID",
			Description: "On the app's Overview page copy the \"Application (client) ID\" (a UUID) and paste it into the wizard.",
			URL:         "https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
			URLLabel:    "Open App registrations",
		},
	}
}
