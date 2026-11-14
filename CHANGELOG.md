# Changelog

All notable changes to **Dux Mail** (`dux-mail`, formerly **DankMail** — `dmail` daemon, `dmail mcp` server, Quickshell UI, and the `dankmailUnread` / **Dux Mail Unread** Dux Shell companion) are documented in this file.

## [0.5.1] - 2026-11-13

### Fixed
- **Dux Shell companion theme tokens, native tooltips, and manifest clarity**:
  - Replaced the hardcoded hex `accountPalette` and `contrastOnDot` in `dms-plugin/DankmailWidget.qml` with harmonized `Theme.accents` (`container` / `onContainer` pairs with `Theme.primary` / `Theme.onPrimary` fallback) so per-account filter dots follow the active Dux Shell theme in both dark and light modes.
  - Replaced the custom `accountTooltip` `Rectangle` and timer in `dms-plugin/DankmailWidget.qml` with `StateLayer` (`qs.DCommon.Widgets`), using Dux Shell's built-in tooltip host and state/ripple feedback.
  - Replaced remaining literal pixel sizes and margins in `dms-plugin/DankmailWidget.qml` with `Theme.*` tokens and `popoutHeight`-derived layout bounds.
  - Updated `dms-plugin/plugin.json` and `dms-plugin/README.md` to clearly describe the bar badge, per-account filter, and triage popout while noting that the Dux Mail desktop app and `dmail` daemon are installed separately.

## [0.5.0] - 2026-11-13

### Added
- **Renamed to Dux Mail (`dux-mail`) with zero-breakage migration**:
  - Renamed application branding, desktop entry (`org.arqueon.duxmail.desktop`), icons (`duxmail.svg`, `duxmail-tray.svg`), Go module (`github.com/arqueon/dux-mail/core`), repository (`arqueon/dux-mail`), and AUR packages (`dux-mail`, `dux-mail-git`, `dms-shell-plugin-dux-mail` with `provides`/`conflicts`/`replaces` for `dankmail*`).
  - Preserved `dmail` binary, `dmail.service`, and internal DMS widget ID `"dankmailUnread"`, with automatic fallback to existing `~/.config/dankmail`, `~/.local/share/dankmail`, keyring service `dankmail`, and a `dankmail.sock -> duxmail.sock` socket symlink.
- **Native Model Context Protocol server (`dmail mcp`) & headless agent backend**:
  - Added `dmail mcp` (`stdio` JSON-RPC 2.0, protocol `2024-11-05`) exposing 27 tools across accounts, local/remote search, thread & message inspection, silent attachment fetching/saving, triage operations, contacts, drafts, and sending, with `--mode full` (default), `draft`, `draft-strict`, and `readonly` permission profiles.
  - Wrapped untrusted message bodies, snippets, and attachment text previews in `<untrusted_email_content>` / `<untrusted_attachment_content>` tags and ensured zero secret/token exposure over IPC or MCP.
  - Added daemon auto-start in `dmail mcp` and headless mode in `dmail run` (skipping Quickshell `qs` launch when `WAYLAND_DISPLAY` and `DISPLAY` are unset, e.g., over SSH).
  - Added silent `attachments.fetch` IPC (`base64` / cache path + automatic UTF-8 `textPreview` for text/Markdown/CSV/JSON/XML/HTML attachments) alongside interactive `attachments.open` and `attachments.saveAs`.
  - Added server-side full-history `SearchRemote` (`provider.RemoteSearcher`) for **Microsoft Graph** (`$search` KQL with message-to-conversation grouping) and **IMAP** (`UID SEARCH CHARSET UTF-8 TEXT` with `ASCII` fallback and thread grouping), completing parity with Gmail remote search.
- **Rich Markdown & HTML email sending, draft formatting toolbar, and `dmail send` CLI**:
  - Upgraded `core/internal/mailmime` with full Markdown-to-HTML rendering (headings `#..######`, bold, italic, strikethrough `~~`, links, nested bulleted/numbered lists, blockquotes, inline & fenced code blocks, horizontal dividers, and aligned pipe tables) and `SanitizeOutgoingHTML` for custom `htmlBody` payloads (stripping scripts/iframes/forms/event handlers/`javascript:` URLs and auto-distilling a `text/plain` fallback when `body` is omitted).
  - Added `htmlBody` and `format` (`auto` | `markdown` | `html` | `plain`) across `ops.compose`, `ops.reply`, `ops.forward`, `drafts.save`, local `drafts.json` persistence (preserving local Markdown source across Gmail `MergeRemote` syncs), and MCP compose/reply/forward/draft tools.
  - Added `dmail send` CLI (`--account`, `--to`, `--cc`, `--bcc`, `--subject`, `--body`, `--body-file`, `--html`, `--html-file`, `--format`, `--attach`).
  - Upgraded `ComposeModal`, inline quick reply in `TriageWindow`, and the **Drafts** detail pane with a full Markdown formatting toolbar, `Markdown` / `HTML` / `Plain text` mode selector, sanitized rich preview, format badges, and one-click **Send** from saved drafts.
- **Compact filterable account dots in the DMS bar companion**:
  - Replaced the vertical per-account text rows in `dms-plugin/DankmailWidget.qml` with compact `20×20` color-coded filterable dots with unread hover tooltips, matching the desktop triage window.

## [0.4.1] - 2026-11-12

### Fixed
- **Dux Shell 2.0+ `DCommon` widget migration (`#14`)**:
  - Migrated `dms-plugin/DankmailWidget.qml` and `dms-plugin/DankmailSettings.qml` from the deprecated `qs.Widgets` `Dank*` shims (`DankActionButton`, `DankFlickable`, `DankIcon`) to `DActionButton`, `DFlickable`, and `DIcon` from `qs.DCommon.Widgets`.
  - Updated `dms-plugin/plugin.json` (`requires_dms: ">=2.0.0"`) and `dms-plugin/README.md` so DMS 1.x installations are not offered an incompatible companion update.

## [0.4.0] - 2026-11-12

### Added
- **One-click OAuth (`dankcalendar` parity), CLI account setup & account filter tooltips**:
  - Added Step 0 one-click sign-in (`Connect Google Account` / `Connect Microsoft Account`) with PKCE S256 in `AccountAddModal`, reusable OAuth client credential resolution (system keyring + `0600` local fallback + existing accounts + env + `-ldflags -X`), and a native `Import client_secret.json` file picker button.
  - Added `dmail account setup google|microsoft` and `dmail account add google|microsoft|imap` CLI commands, plus hover tooltips (`DankTooltip`) showing each account's email address and unread count on the header account filter dots.
- **Full IMAP + SMTP provider**:
  - Added `core/internal/provider/imap` (`go-imap/v2` + `net/smtp` STARTTLS/TLS, with loopback self-signed TLS tolerance for Proton Mail Bridge) supporting sync, archive/sent pagination, flag/folder triage, replies, compose, and attachments, automatically un-parking previously stored IMAP accounts.
- **OS-native attachment opening & Save as**:
  - Clicking an attachment chip downloads the part on demand into `~/.cache/dankmail/attachments/` (`0600`) and opens it via `xdg-open` (`attachments.open`), with a secondary **Save as…** folder picker button (`attachments.saveAs`) and webmail fallback on error.
- **Snoozed mailbox view, weekend preset & Move to Inbox**:
  - Added a **Snoozed** filter chip ordered by wake time (`SnoozedUntil ASC`), wake-time badges on snoozed threads, **Unsnooze** actions (`ops.unsnooze`), and a **This weekend** (Saturday 09:00) snooze preset, wrapping inline row snooze presets within the thread column margins so options never clip at the window edge.
  - Added **Move to Inbox** (`ops.unarchive`) across the preview toolbar, thread row hover actions, bulk selection bar, and the `dankmailUnread` bar popout for archived and other non-inbox threads, flushing pending undoable actions immediately on filter switches or window close.
- **HTML-to-Markdown message distillation & full body resync**:
  - Added `core/internal/mailtext` (`html-to-markdown/v2`) across Gmail, Microsoft Graph, and IMAP so HTML messages retain readable link labels, headings, lists, and multi-column data tables while unwrapping layout tables, sanitizing `<img>` / linked-banner nodes, and stripping hidden preheaders and tracking pixels, plus a per-account full resync button in `SettingsModal`.
  - Replaced regex-only Markdown link/image parsing in `BodyFormatter.js` with balanced token scanners so cached and incoming links/images with optional titles, parentheses or spaces in URLs, multi-line block wrappers, and `data:` URIs never leak raw `!` or `(...)` syntax.
- **Selectable & copyable `Subject`**:
  - Replaced static subject labels in thread and draft previews with read-only mouse-selectable `TextEdit` controls and one-click clipboard copy buttons.

### Fixed
- **Dux Shell (`dms-theme-sync`) theme roles, typography, and WCAG contrast**:
  - Updated `Theme.qml` to watch `~/.cache/dms/dms-colors.json` and `~/.cache/dms/dms-typography.json` (with `~/.cache/DankMaterialShell/` fallback), honor `"mode": "dark"|"light"`, load full Material 3 `on_*` / `secondary_container` / `tertiary_container` / `inverse_surface` roles, and enforce WCAG contrast safeguards.
  - Replaced all 30 `primaryContainer`+`primary` low-contrast buttons/chips across `quickshell/` and `dms-plugin/` with `Theme.primary`+`Theme.primaryText` (filled actions) or `Theme.primaryContainer`+`Theme.onPrimaryContainer` (tonal chips), and removed hardcoded hex colors in `TriageWindow.qml`.

## [0.3.12] - 2026-10-15

### Added
- **Drafts section & draft editing**:
  - Added a **Drafts** view in both the main `TriageWindow` filter bar and the `dankmailUnread` DankMaterialShell bar popout.
  - Added local draft persistence (`drafts.json`) synchronized with Gmail `users.drafts` (`ListDrafts`, `GetDraft`, `CreateDraft`, `UpdateDraft`, `DeleteDraft`) over IPC (`drafts.list`, `drafts.save`, `drafts.delete`, `ui.editDraft`).
  - Clicking a draft in the bar popout or clicking **Edit draft** in the triage window opens `ComposeModal` pre-populated with its recipients (`To`, `Cc`, `Bcc`), subject, body, and attachments.
- **Rich composer, URL links, `Cc`/`Bcc`, and file attachments**:
  - Added `Cc / Bcc` recipient fields with contact autocomplete, a formatting and URL link insertion toolbar (`**bold**`, `*italic*`, `[label](url)`, bulleted/numbered lists, quotes, code, horizontal rules), and a live rich-text **Preview** toggle in `ComposeModal`.
  - Added a native DankMail-styled multi-file attachment picker (`FilePickerModal.qml` backed by `fs.list` IPC) in both `ComposeModal` and inline quick reply, avoiding unstyled `QtQuick.Dialogs` fallbacks under `QT_QUICK_CONTROLS_STYLE=Material`.
  - Updated `mailmime` to build `multipart/mixed` base64 attachments and `multipart/alternative` (`text/plain` + `text/html`) when messages contain Markdown links or formatting.
- **Thread message selector & copyable sender/recipient addresses**:
  - Added a horizontal message selector bar in the preview pane for multi-message threads so any message in a conversation can be inspected directly.
  - Added RFC 2047 MIME word decoding (`=?utf-8?q?...?=`, `=?utf-8?b?...?=`) and interactive address chips for sender (`From`) and recipients (`To`, `Cc`) with one-click clipboard copy and a **Copy all emails** action.

### Fixed
- **Reader formatting & link activation**:
  - Unwrapped Gmail redirect URLs (`https://www.google.com/url?q=...`), merged soft-wrapped `label <https://...>` plain-text links, linkified bare domains (such as `plataformas.udgplus.udg.mx`) and `<email@domain>` addresses, and improved multi-line `*...*` emphasis and nested list parsing in `BodyFormatter.js`.
  - Fixed link hover cursor detection and click handling on `TextEdit` (`selectByMouse: true`) using `TapHandler` + `linkAt` and daemon-side `ui.openUrl` (`xdg-open`).
  - Fixed chip width calculations (`Layout.preferredWidth` / `contentWidth` with `Text.NoWrap`) in `RowLayout` headers and suppressed false-positive BCC warnings on forwarded mail when recipients are present.

## [0.3.11] - 2026-10-02

### Added
- Resumable mailbox sync checkpoints across quota pauses and restarts.
- **Sent** mailbox filter with on-demand pagination (`threads.fetchSent`) for Gmail and Microsoft Graph.
- **Not spam** rescue action (`ops.unspam`) for single threads and cross-account bulk selections.

### Fixed
- Companion socket reconnection recovery and translation installation in `dms-plugin`.

## [0.3.10] - 2026-09-30

### Fixed
- Paginated Gmail archive history (`threads.fetchArchived`) so **Archived** loads unstarred archived conversations beyond the local cache.

## [0.3.9] - 2026-09-29

### Added
- **Archived** quick filter in the triage window.
