//@ pragma UseQApplication
import Quickshell
import QtQuick

// dankmail Quickshell entrypoint. The window is a *view* over the daemon:
// closing it hides it; sync and notifications keep running in dmail.
// Anillo 1 wires TrayIcon + TriagePopup through Services/DankMailService.
ShellRoot {
    id: root
}
