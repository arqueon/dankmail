// Package accounts owns account provisioning: the in-app OAuth setup
// guide (served over IPC to the GUI wizard, pattern borrowed from
// dankcalendar) and the finish step shared by the CLI and the wizard.
package accounts

// SetupStep is one step of the guided "bring your own OAuth client"
// wizard. Screenshot filenames are resolved by the GUI relative to its
// assets/gmail-setup directory; missing files degrade silently.
type SetupStep struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	URLLabel    string `json:"urlLabel,omitempty"`
	Screenshot  string `json:"screenshot,omitempty"`
	// Note is an optional callout rendered alongside the step.
	Note string `json:"note,omitempty"`
}

// GmailSetupSteps walks the user through creating their own Google OAuth
// desktop client. Done once. The app starts in testing mode with the user
// as its only test user; the final step publishes it to production because
// Google expires refresh tokens of testing-mode apps every 7 days, which
// would force a re-auth weekly. Publishing needs no verification for
// personal use — the consent screen just shows an "unverified app" warning.
func GmailSetupSteps() []SetupStep {
	return []SetupStep{
		{
			Title:       "Create a Google Cloud project",
			Description: "Pick any name (e.g. \"dankmail\"). You'll only do this once.",
			URL:         "https://console.cloud.google.com/projectcreate",
			URLLabel:    "Open Google Cloud Console",
		},
		{
			Title:       "Enable the Gmail API",
			Description: "With the new project selected, click \"Enable\" on the Gmail API page.",
			URL:         "https://console.cloud.google.com/apis/library/gmail.googleapis.com",
			URLLabel:    "Enable Gmail API",
			Note:        "Ignore the \"Create credentials\" button the console shows afterwards — it steers you toward a service account or API key, which dankmail cannot use. The right credential is an OAuth client ID (Desktop app), created in the later steps after the consent screen is configured.",
		},
		{
			Title:       "Enable the People API",
			Description: "Enable the People API so the two read-only contacts permissions requested by dankmail can feed compose autocomplete. Correspondents found in cached mail are also suggested.",
			URL:         "https://console.cloud.google.com/apis/library/people.googleapis.com",
			URLLabel:    "Enable People API",
		},
		{
			Title:       "Configure the Google Auth Platform",
			Description: "Click \"Get started\" on the overview page. App name: anything (e.g. \"dankmail\"). User support email: your own address. Audience: \"External\". Then agree and finish the initial configuration.",
			URL:         "https://console.cloud.google.com/auth/overview",
			URLLabel:    "Open Google Auth Platform",
		},
		{
			Title:       "Declare the permissions dankmail uses",
			Description: "On Data Access, add gmail.modify, gmail.send, contacts.readonly and contacts.other.readonly. These must match the permissions requested during sign-in; dankmail never asks for the unrestricted mail.google.com scope.",
			URL:         "https://console.cloud.google.com/auth/scopes",
			URLLabel:    "Open Data Access",
			Note:        "Mail modification and sending power triage and replies. Both contacts permissions are read-only and only power recipient suggestions.",
		},
		{
			Title:       "Add yourself as a test user",
			Description: "On the Audience page, under \"Test users\" click \"Add users\" and enter the Gmail address you want dankmail to watch. Only test users can sign in while the app is unpublished.",
			URL:         "https://console.cloud.google.com/auth/audience",
			URLLabel:    "Open Audience page",
		},
		{
			Title:       "Create an OAuth client",
			Description: "On the Clients page click \"Create client\" (in older consoles: Credentials → Create credentials → OAuth client ID), choose \"Desktop app\", and create it. Copy the Client ID and Client Secret for the next step.",
			URL:         "https://console.cloud.google.com/auth/clients",
			URLLabel:    "Open Clients page",
			Note:        "Application type must be \"Desktop app\" — NOT a service account, API key, or web application. The downloaded JSON contains the client ID and desktop-client secret used by the loopback OAuth flow.",
		},
		{
			Title:       "Publish the app (skip the 7-day token expiry)",
			Description: "On the Audience page click \"Publish app\" and confirm. Google expires the tokens of testing-mode apps every 7 days, which would make dankmail ask you to re-authenticate weekly. Do NOT submit for verification — for personal use it isn't needed.",
			URL:         "https://console.cloud.google.com/auth/audience",
			URLLabel:    "Open Audience page",
			Note:        "After publishing, the Google consent screen shows \"Google hasn't verified this app\" — click Advanced, then \"Go to dankmail (unsafe)\" and continue. That warning is expected for a personal unverified app and only appears during consent.",
		},
	}
}
