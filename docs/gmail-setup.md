# Gmail account setup

dankmail talks to Gmail through the official API with **your own OAuth
client**. You create a (free) client ID in Google Cloud Console once.
The app starts in testing mode with you as the only test user; the last
step publishes it to production **without** submitting it for Google
verification — for personal use none is needed.

**The easiest path is the in-app wizard** (tray → Open Dank Mail → the
person-add button, or the "Add a Gmail account" button when no account
exists): it walks you through the exact steps below with direct links,
takes the Client ID/Secret, and runs the OAuth consent — no environment
variables needed. Credentials and tokens land in your system keyring,
stored per account. The steps are served by the daemon over IPC
(`accounts.gmail.setupGuide`), same pattern as dankcalendar.

APIs to enable:

- **Gmail API** — mail triage, replies, and compose
- **People API** — read-only recipient autocomplete

Scopes requested:

- `gmail.modify` — read messages, change labels (read/star/archive/trash)
- `gmail.send` — send replies and new messages
- `contacts.readonly` — suggest saved Google contacts
- `contacts.other.readonly` — suggest Google “Other contacts”

The full-access scope (`https://mail.google.com/`) is never used.

## Steps (what the wizard walks you through)

1. Create a project at <https://console.cloud.google.com/projectcreate>
   (any name, e.g. "dankmail").
2. Enable the **Gmail API**:
   <https://console.cloud.google.com/apis/library/gmail.googleapis.com>.
   ⚠️ Ignore the "Create credentials" button the console offers right
   after enabling — it steers you toward a **service account** or API
   key, neither of which works for a personal mailbox. The credential
   dankmail needs is an **OAuth client ID (Desktop app)**, created in
   step 5 once the consent screen exists.
3. Enable the **People API**:
   <https://console.cloud.google.com/apis/library/people.googleapis.com>.
4. Configure the Google Auth Platform
   (<https://console.cloud.google.com/auth/overview>): app name anything,
   support email your own, audience **External**.
5. On **Data Access** (<https://console.cloud.google.com/auth/scopes>),
   add `gmail.modify`, `gmail.send`, `contacts.readonly`, and
   `contacts.other.readonly`. The configured scopes must match those
   requested during sign-in.
6. Add yourself as **test user** on the Audience page
   (<https://console.cloud.google.com/auth/audience>).
7. Create an OAuth client of type **Desktop app**
   (<https://console.cloud.google.com/auth/clients>) and copy the
   Client ID and Client Secret.
8. **Publish the app** on the Audience page ("Publish app" → confirm;
   do NOT submit for verification). Google expires the refresh tokens
   of testing-mode apps every **7 days**, which would force a weekly
   re-auth. After publishing, the consent screen shows "Google hasn't
   verified this app" — click Advanced → "Go to dankmail (unsafe)";
   that warning is expected and only appears during consent.
9. Enter them in the wizard and authorize in the browser — or, for the
   CLI path, export `DMAIL_GOOGLE_CLIENT_ID` /
   `DMAIL_GOOGLE_CLIENT_SECRET` and run `dmail account add-gmail`.

If a token does expire or is revoked (`auth_error` on the account), the
key button in Settings → Accounts re-runs the consent with the stored
client — no need to re-enter credentials. CLI: `dmail account reauth`.

The account's address is read from the authorized Gmail profile (you
never type it), and the token plus your OAuth client are stored in the
system keyring per account — so token refresh works no matter how the
daemon is started (systemd, terminal, etc.).
