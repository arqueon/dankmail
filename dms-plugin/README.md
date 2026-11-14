# Dux Mail for Dux Shell (DankMaterialShell)

This directory contains the **Dux Shell / DankMaterialShell companion for
[Dux Mail](https://github.com/arqueon/dux-mail)** (formerly `dankmail`). The suite is composed of this
DMS bar plugin (`dankmailUnread` / **Dux Mail Unread**) and additionally the Dux Mail desktop application, both powered
by the `dmail` daemon; it is not a separate mail client or a second
implementation.

The plugin adds Dux Mail to the bar with live unread status, compact filterable
per-account dots with unread tooltips, a popout with up to 20 recent Inbox, Starred, or Drafts entries, triage actions, compose and sync
controls, do-not-disturb status, and a shortcut into Dux Mail's quick
reply and rich Markdown/HTML draft-editing flows. Mail access, synchronization, account state, MCP agent tools, and mutations remain
owned by the `dmail` daemon.

## Requirements

- Dux Shell (DankMaterialShell) 2.0.0 or later.
- Dux Mail (`dux-mail`) installed and configured, including the `dmail` daemon.
- At least one mail account configured in Dux Mail.

## Installation

Install **Dux Mail Unread** from the Dux Shell plugin browser. The
registry entry points to this `dms-plugin/` directory in the main `dux-mail`
repository, so plugin and daemon development stay together.

On Arch Linux, you can instead install the AUR package:

```sh
paru -S dms-shell-plugin-dux-mail
```

(Upgrading from `dms-shell-plugin-dankmail` replaces the legacy package automatically while preserving your `dankmailUnread` bar placement and settings.)

This installs the companion in DMS's system plugin directory (`/etc/xdg/quickshell/dms-plugins/dankmailUnread`). An existing
user-installed copy takes precedence; move that copy out of
`~/.config/DankMaterialShell/plugins/` if you want to use the packaged version.
Enable **Dux Mail Unread** in DMS Settings → Plugins and add it to the bar.

For a local checkout, install or link this directory as the plugin source:

```sh
make install-dms-plugin
# or symlink directly for development:
ln -s /path/to/dux-mail/dms-plugin \
  ~/.config/DankMaterialShell/plugins/dankmailUnread
```

Then enable `dankmailUnread` in DMS and make sure the user service is running:

```sh
systemctl --user enable --now dmail
```

See the [main Dux Mail README](https://github.com/arqueon/dux-mail#readme) for
account setup, `dmail mcp` agent integration, packaging, and daemon documentation.

## Source of truth

The canonical plugin source is this directory. Note that the internal DMS plugin
identifier remains `"dankmailUnread"` so existing user bar layouts and settings
migrate across the Dux Mail rename without manual reconfiguration.

## Controls and connection status

Left click opens the popout, middle click shows or hides the app, and right
click requests synchronization. Hover over an account dot or action button for its label.
Click an account dot to filter the popout to that account; click again to show all accounts.
The badge counts unread Inbox mail even when the popout shows Starred or Drafts.
English is the source language; the plugin includes Spanish translations
selected by DMS. The desktop application's language setting is separate.

If the daemon disconnects, the plugin clears its old mail rows and retries
over `$XDG_RUNTIME_DIR/duxmail.sock`.
A failed request shows an error instead of claiming that the mailbox is empty;
open Dux Mail to inspect the account and retry. The event subscription reconnects
independently if only that connection fails. A safety poll runs every minute.

Both `make install-dms-plugin` and the packaged plugin install `translations/`.
When updating a local copy or symlink, run `dms ipc plugin-scan reload dankmailUnread`.
