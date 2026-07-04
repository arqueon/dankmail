package accounts

// IMAPPreset pre-fills the IMAP form for well-known providers. The Note
// warns about app-specific passwords where the provider requires them.
type IMAPPreset struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Security   string `json:"security"`
	SMTPHost   string `json:"smtpHost"`
	SMTPPort   int    `json:"smtpPort"`
	WebmailURL string `json:"webmailUrl,omitempty"`
	Note       string `json:"note,omitempty"`
	NoteURL    string `json:"noteUrl,omitempty"`
}

// IMAPPresets covers the common non-Gmail providers; "custom" leaves the
// form blank.
func IMAPPresets() []IMAPPreset {
	return []IMAPPreset{
		{
			Key: "icloud", Label: "iCloud Mail",
			Host: "imap.mail.me.com", Port: 993, Security: "tls",
			SMTPHost: "smtp.mail.me.com", SMTPPort: 587,
			WebmailURL: "https://www.icloud.com/mail",
			Note:       "iCloud requires an app-specific password (not your Apple ID password).",
			NoteURL:    "https://support.apple.com/102654",
		},
		{
			Key: "outlook", Label: "Outlook / Hotmail",
			Host: "outlook.office365.com", Port: 993, Security: "tls",
			SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587,
			WebmailURL: "https://outlook.live.com/mail",
			Note:       "Personal Outlook accounts may need an app password with two-step verification enabled.",
			NoteURL:    "https://support.microsoft.com/account-billing/9bf6bb53-7c1f-48f9-9b21-c62d7a0e6b7c",
		},
		{
			Key: "yahoo", Label: "Yahoo Mail",
			Host: "imap.mail.yahoo.com", Port: 993, Security: "tls",
			SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 587,
			WebmailURL: "https://mail.yahoo.com",
			Note:       "Yahoo requires an app password generated in your account security settings.",
			NoteURL:    "https://help.yahoo.com/kb/SLN15241.html",
		},
		{
			Key: "fastmail", Label: "Fastmail",
			Host: "imap.fastmail.com", Port: 993, Security: "tls",
			SMTPHost: "smtp.fastmail.com", SMTPPort: 587,
			WebmailURL: "https://app.fastmail.com",
			Note:       "Fastmail requires an app password for third-party clients.",
			NoteURL:    "https://www.fastmail.help/hc/en-us/articles/360058752854",
		},
		{
			Key: "proton", Label: "Proton Mail (Bridge)",
			Host: "127.0.0.1", Port: 1143, Security: "starttls",
			SMTPHost: "127.0.0.1", SMTPPort: 1025,
			WebmailURL: "https://mail.proton.me",
			Note:       "Proton Mail needs the Proton Mail Bridge running locally; use the credentials Bridge shows you.",
			NoteURL:    "https://proton.me/mail/bridge",
		},
		{
			Key: "custom", Label: "Custom IMAP server",
		},
	}
}
