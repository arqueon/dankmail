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
// desktop client. Done once; the app stays in testing mode with the user
// as its only test user, so Google verification never applies.
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
			Title:       "Enable the People API (optional)",
			Description: "Also enable the People API if you want your Google contacts in the compose autocomplete. Skipping it is fine: suggestions then come only from people you've corresponded with.",
			URL:         "https://console.cloud.google.com/apis/library/people.googleapis.com",
			URLLabel:    "Enable People API",
		},
		{
			Title:       "Configure the Google Auth Platform",
			Description: "Click \"Get started\" on the overview page. App name: anything (e.g. \"dankmail\"). User support email: your own address. Audience: \"External\". Then agree and finish — nothing here is ever published.",
			URL:         "https://console.cloud.google.com/auth/overview",
			URLLabel:    "Open Google Auth Platform",
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
			Note:        "Application type must be \"Desktop app\" — NOT a service account, API key, or web application. dankmail will only request the gmail.modify and gmail.send scopes — never full mailbox access.",
		},
	}
}
