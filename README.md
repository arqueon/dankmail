# 📬 dankmail

> **Triage your inbox, preserve your focus.**  
> A lightning-fast, keyboard-driven mail triage system for Linux. Unified multi-account inbox, local-first cache, and distraction-free plain-text markdown distiller — wrapped in a dynamic Material Design shell.

<p align="center">
  <img src="assets/screenshot.png" alt="dankmail triage window" width="550" style="border-radius: 8px; box-shadow: 0 4px 20px rgba(0,0,0,0.15);">
</p>

---

## ⚡ The Philosophy

`dankmail` is **not a mail client** — it is a **mail controller**. 

Modern email clients are bloated, distract you with HTML tracking pixels, and suck you into endless threads. `dankmail` is built for power users who want to triage their mail, clear their inbox in seconds, and get back to work:

*   **Keyboard-driven flow:** Archive, delete, star, snooze, and reply in milliseconds.
*   **Zero distractions:** Distills complex HTML mail into clean, safe markdown. No web engines, no tracking scripts, no slow loading times.
*   **Local-first speed:** Everything runs over a local SQLite cache. Operations are optimistic and happen instantly, queued in the background.
*   **Invisible by default:** Lives in your system tray and starts instantly with a global hotkey or system bar trigger.

---

## ✨ Features at a Glance

### 🚀 Local-First Sync Engine
*   **Official Gmail API & Graph API:** Syncs using secure modern endpoints (not legacy IMAP).
*   **Optimistic Operation Queue:** Actions execute locally *instantly*, then sync to remote servers with exponential backoff and batch coalescing.
*   **Thread Freezing:** If sync occurs while you have pending actions, local edits are frozen so remote data never overwrites your current work.
*   **30-Day Auto-Janitor:** Automatically prunes local SQLite cache while keeping starred and snoozed threads safe.

### 🛡️ Distraction-Free Triage Window
*   **HTML to Markdown Distiller:** Reads clean, stylized markdown. Indents quote chains with color coding and renders text links safely.
*   **Attachment Metadata Chips:** View file names and sizes instantly; opens webmail on click to keep heavy binary downloads off your machine.
*   **Quick Reply & Compose:** Write lightning-fast replies in plain text with full thread nesting (`In-Reply-To`/`References`).
*   **BCC Helper:** Warns you dynamically when you've received mail via BCC (your address isn't in To/Cc).
*   **Scrollable Recipients Header:** The To/Cc/Bcc recipient list adjusts automatically with text wrapping inside a custom `DankFlickable` panel, maintaining a clean header geometry regardless of the number of recipients.

### ⏳ Temporal Undo (Safety Net)
*   **40-Second Delay:** Destructive actions (Archive, Delete, Snooze, Unspam) are queued in the client with a 40-second grace period.
*   **Material 3 Undo Banner:** Shows a floating banner at the bottom with a prominent "Undo" button. Click it to instantly restore the thread's state.

### 🎨 Native Shell Integration
*   **DankMaterialShell Plugin:** Live unread count capsules, interactive popouts with status-bar widgets, and system D-Bus notification action buttons.
*   **Dynamic Theme Sync:** Follows dynamic Material Design system colors (`dms-colors.json`).
*   **DMAIL_LANG Locale:** follows system language settings for Spanish (`es`) and English (`en`).

---

## 🛠️ Scripting & IPC (Unix Socket)

`dankmail` is fully scriptable. A background Go daemon exposes a unix-socket IPC (`$XDG_RUNTIME_DIR/dankmail.sock`) sending line-delimited JSON.

```bash
# Toggle the main triage window from a compositor shortcut (e.g. Niri / sway)
dmail toggle

# Query unread threads in JSON format
dmail list --unread --json

# Trigger a manual sync on all accounts
dmail sync
```

---

## 📦 Installation

### Arch Linux (AUR)

Install the stable release or the development package (`-git` follows the latest commits on `main`):

```bash
# Stable Release
paru -S dankmail
systemctl --user enable --now dmail

# Git Version
paru -S dankmail-git
systemctl --user enable --now dmail
```

### From Source

Ensure you have **Go ≥ 1.22** and [Quickshell](https://quickshell.org) installed on your system:

```bash
git clone https://github.com/arqueon/dankmail && cd dankmail
make build
make install PREFIX=~/.local
make install-systemd PREFIX=~/.local
systemctl --user enable --now dmail
```

---

## 🔑 Account Setup

*   **Gmail**: Open the triage window, click the settings cog, and follow the guided step-by-step assistant. It sets up your secure client ID and consent screen using minimal scopes (`gmail.modify` + `gmail.send`). Read [docs/gmail-setup.md](docs/gmail-setup.md) for details.
*   **Microsoft**: Connect Microsoft 365, Outlook, or Hotmail accounts using the Graph API:
    ```bash
    dmail account add-microsoft --client-id <azure-client-uuid>
    ```
*   **Generic IMAP**: Fastmail, iCloud, Yahoo, and Proton Bridge presets are supported and parked in Ring 2.

---

## 📜 License

Distributed under the **GPL-3.0-or-later** license. See [LICENSE](LICENSE) for details.  
UI components utilize modified parts of the MIT-licensed infrastructure from `dankcalendar` (Avenge Media LLC), preserved in `quickshell/NOTICE`.
