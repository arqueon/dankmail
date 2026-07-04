# Gmail account setup

dankmail talks to Gmail through the official API with **your own OAuth
client**. You create a (free) client ID in Google Cloud Console once;
dankmail never sees Google's verification process because the app runs
in testing mode with you as the only test user.

**The easiest path is the in-app wizard** (tray → Open Dank Mail → the
person-add button, or the "Add a Gmail account" button when no account
exists): it walks you through the exact steps below with direct links,
takes the Client ID/Secret, and runs the OAuth consent — no environment
variables needed. Credentials and tokens land in your system keyring,
stored per account. The steps are served by the daemon over IPC
(`accounts.gmail.setupGuide`), same pattern as dankcalendar.

Scopes requested — and the only ones dankmail will ever request:

- `gmail.modify` — read messages, change labels (read/star/archive/trash)
- `gmail.send` — send replies and new messages

The full-access scope (`https://mail.google.com/`) is never used.

## Steps (what the wizard walks you through)

1. Create a project at <https://console.cloud.google.com/projectcreate>
   (any name, e.g. "dankmail").
2. Enable the **Gmail API**:
   <https://console.cloud.google.com/apis/library/gmail.googleapis.com>.
3. Configure the Google Auth Platform
   (<https://console.cloud.google.com/auth/overview>): app name anything,
   support email your own, audience **External**. Nothing is published.
4. Add yourself as **test user** on the Audience page
   (<https://console.cloud.google.com/auth/audience>).
5. Create an OAuth client of type **Desktop app**
   (<https://console.cloud.google.com/auth/clients>) and copy the
   Client ID and Client Secret.
6. Enter them in the wizard and authorize in the browser — or, for the
   CLI path, export `DMAIL_GOOGLE_CLIENT_ID` /
   `DMAIL_GOOGLE_CLIENT_SECRET` and run `dmail account add-gmail`.

The account's address is read from the authorized Gmail profile (you
never type it), and the token plus your OAuth client are stored in the
system keyring per account — so token refresh works no matter how the
daemon is started (systemd, terminal, etc.).
