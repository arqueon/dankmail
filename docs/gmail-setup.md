# Gmail account setup

dankmail talks to Gmail through the official API with **your own OAuth
client**. You create a (free) client ID in Google Cloud Console once;
dankmail never sees Google's verification process because the app runs
in testing mode with you as the only test user.

Scopes requested — and the only ones dankmail will ever request:

- `gmail.modify` — read messages, change labels (read/star/archive/trash)
- `gmail.send` — send replies and new messages

The full-access scope (`https://mail.google.com/`) is never used.

## Steps (to be expanded during Anillo 1)

1. Create a project at <https://console.cloud.google.com/>.
2. Enable the **Gmail API** for the project.
3. Configure the OAuth consent screen: type **External**, publishing
   status **Testing**, add your own address as test user.
4. Create credentials → **OAuth client ID** → application type
   **Desktop app**.
5. Export the client ID/secret to the daemon environment
   (`DMAIL_GOOGLE_CLIENT_ID`, `DMAIL_GOOGLE_CLIENT_SECRET`) or enter
   them in Settings → Accounts.
6. Add the account in dankmail; the browser opens the consent page and
   the token lands in your system keyring, never on disk.
