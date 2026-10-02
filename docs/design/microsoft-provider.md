# Microsoft provider: Outlook.com / Microsoft 365

Status (2026-10-02): implemented. Personal and organizational accounts use
Microsoft Graph through the shared OAuth broker, IPC account wizard and provider
registry. The desktop client uses authorization code + PKCE and the `common`
tenant. It does not require a client secret.

## Application registration

Create an application in Microsoft Entra **App registrations**:

- Name: `DankMail Desktop` (or another recognizable name).
- Supported accounts: organizational directories **and personal Microsoft accounts**.
- Platform: **Mobile and desktop applications**.
- Redirect URI: `http://localhost/callback`. The runtime chooses a loopback port.
- Enable public client flows. Do not create a client secret.
- Delegated Graph permissions: `Mail.ReadWrite`, `Mail.Send`, `User.Read`;
  the OAuth request also includes `offline_access` for refresh tokens.
- Paste the **Application (client) ID** into DankMail's Microsoft account wizard.
  Complete sign-in and consent in the system browser.

The registration must live in a directory where the signed-in user can register
applications. A personal mailbox can authorize the application, but that alone
does not guarantee access to an Entra directory for creating the registration.
If the portal requests verification or directory access, complete that prerequisite
before claiming that registration or mailbox connection succeeded.

References: [desktop registration](https://learn.microsoft.com/en-us/entra/identity-platform/quickstart-register-app),
[redirect URI rules](https://learn.microsoft.com/en-us/entra/identity-platform/reply-url).

## Storage and synchronization

Tokens and the client ID use DankMail's existing keyring broker; no secrets belong
in this document or the repository. A conversation ID identifies a thread within
its account. Operations never mix provider IDs from separate accounts.

Inbox and Junk folder delta streams drive background synchronization. Completed
cursors retain the per-folder delta-link JSON format. Interrupted rounds use a
versioned checkpoint containing only account identity, folder/page continuation
and outstanding conversation IDs. Each downloaded batch and its cursor are saved
in the same SQLite transaction. Only the final batch prunes a full snapshot.
Expired delta links restart a full round. Folder lookup failures are retried rather
than permanently cached.

Graph requests use immutable message IDs and honor `Retry-After` for 429/503
responses. While the cooldown is active, the same client defers subsequent calls.
Continuation URLs must remain on `https://graph.microsoft.com`. Writes are not
silently replayed by the HTTP client.

References: [throttling](https://learn.microsoft.com/en-us/graph/throttling),
[immutable IDs](https://learn.microsoft.com/en-us/graph/outlook-immutable-id).

## Sent and triage

The Sent view requests the newest 25 Sent Items message references, ordered by
`sentDateTime desc`, and fetches their conversations. Older pages are explicit;
failed pages preserve unfinished conversation IDs. Backfill does not generate
new-mail notifications or alter the background delta cursor. Cached Sent threads
are sorted by their most recent outgoing message, and the reader shows that
message, including its matching sender, recipients and date.

Read/unread and star/unstar patch the messages. Archive, trash, spam and not-spam
move messages between well-known folders. Not-spam moves Junk messages to Inbox.
Reply and compose use the shared MIME builder and Graph sendMail endpoint. These
operations are queued locally with optimistic state and failure recovery.

## Current limits

Archive browsing and full-mailbox remote text search remain Gmail-only. Microsoft
Sent supports remote pagination. Truly deleted message IDs from delta responses
cannot always be mapped back to a cached conversation; a later full resync cleans
stale monitored threads. Attachment contents and original HTML remain in webmail;
DankMail stores text and attachment metadata, and never loads remote message images.

Live account setup must be verified separately from fixture tests; passing unit
or transport tests does not prove that a user's Microsoft account is connected.
