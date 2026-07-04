//@ pragma Env QSG_RENDER_LOOP=threaded
//@ pragma Env QT_WAYLAND_DISABLE_WINDOWDECORATION=1
//@ pragma Env QT_QUICK_CONTROLS_STYLE=Material
//@ pragma UseQApplication
//@ pragma AppId org.arqueon.dankmail

import QtQuick
import Quickshell
import Quickshell.Wayland
import qs.Common
import qs.Modules
import qs.Services

// dankmail Quickshell entrypoint (structure mirrors dankcalendar's).
// The window is a view over the dmail daemon: closing it only hides it;
// sync and notifications keep running.
ShellRoot {
    id: root

    readonly property var log: Log.scoped("Shell")

    function focusToplevel() {
        const toplevels = ToplevelManager.toplevels;
        if (!toplevels || !toplevels.values)
            return;
        for (const t of toplevels.values) {
            if (t.appId === "org.arqueon.dankmail") {
                t.activate();
                return;
            }
        }
    }

    function showAndFocus() {
        if (windowLoader.active) {
            focusToplevel();
            return;
        }
        windowLoader.active = true;
        focusRetry.restart();
    }

    function handleWindowAction(action) {
        switch (action) {
        case "show":
            showAndFocus();
            break;
        case "hide":
            windowLoader.active = false;
            break;
        case "toggle":
            if (windowLoader.active) {
                windowLoader.active = false;
            } else {
                showAndFocus();
            }
            break;
        }
    }

    // Pending intents (dcal pattern): the window may not exist yet when
    // an open-thread/compose event arrives; stash and apply on load.
    property int pendingThreadId: -1
    property bool pendingCompose: false

    function applyPending() {
        if (!windowLoader.item)
            return;
        if (pendingThreadId >= 0) {
            windowLoader.item.openThread(pendingThreadId);
            pendingThreadId = -1;
        }
        if (pendingCompose) {
            windowLoader.item.openCompose();
            pendingCompose = false;
        }
    }

    Connections {
        target: DankMailService
        function onWindowActionRequested(action) {
            root.handleWindowAction(action);
        }
        function onShowThreadRequested(threadId) {
            root.pendingThreadId = threadId;
            root.showAndFocus();
            root.applyPending();
        }
        function onComposeRequested() {
            root.pendingCompose = true;
            root.showAndFocus();
            root.applyPending();
        }
    }

    Connections {
        target: windowLoader
        function onItemChanged() {
            root.applyPending();
        }
    }

    Timer {
        id: focusRetry
        interval: 150
        repeat: false
        onTriggered: root.focusToplevel()
    }

    Loader {
        id: trayLoader
        active: Quickshell.env("DMAIL_NO_TRAY") !== "1"
        source: Qt.resolvedUrl("Modules/TrayIcon.qml")
        onLoaded: item.shell = root
        onStatusChanged: {
            if (status === Loader.Error)
                root.log.warn("tray icon unavailable (Qt.labs.platform missing?)");
        }
    }

    LazyLoader {
        id: windowLoader
        active: Quickshell.env("DMAIL_START_HIDDEN") !== "1"

        TriageWindow {
            onHideRequested: windowLoader.active = false
        }
    }
}
