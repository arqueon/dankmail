import QtQuick
import Qt.labs.platform as Platform
import qs.Common
import qs.Services

Item {
    id: root

    property var shell: null

    function showWindow() {
        if (root.shell)
            root.shell.handleWindowAction("show");
    }

    function toggleWindow() {
        if (root.shell)
            root.shell.handleWindowAction("toggle");
    }

    // The SNI item id is read from the application name at registration
    // time, so rename before the icon first becomes visible.
    Component.onCompleted: {
        Qt.application.name = "dank-mail";
        Qt.application.displayName = "Dank Mail";
        tray.visible = true;
    }

    Platform.SystemTrayIcon {
        id: tray
        visible: false
        tooltip: DankMailService.unreadTotal > 0
            ? "Dank Mail — " + DankMailService.unreadTotal + " " + I18n.tr("unread", "tray tooltip")
            : "Dank Mail"
        icon.source: Qt.resolvedUrl("../assets/dankmail-tray.svg")

        onActivated: reason => {
            switch (reason) {
            case Platform.SystemTrayIcon.Trigger:
            case Platform.SystemTrayIcon.DoubleClick:
                root.toggleWindow();
                break;
            }
        }

        menu: Platform.Menu {
            Platform.MenuItem {
                text: I18n.tr("Open Dank Mail", "tray menu")
                onTriggered: root.showWindow()
            }

            Platform.MenuItem {
                text: I18n.tr("Sync now", "tray menu")
                onTriggered: DankMailService.syncNow()
            }

            Platform.MenuItem {
                text: DankMailService.dndEnabled
                    ? I18n.tr("Disable do-not-disturb", "tray menu")
                    : I18n.tr("Enable do-not-disturb", "tray menu")
                onTriggered: DankMailService.setDnd(!DankMailService.dndEnabled)
            }

            Platform.MenuItem {
                separator: true
            }

            Platform.MenuItem {
                text: I18n.tr("Quit", "tray menu")
                onTriggered: DankMailService.quit()
            }
        }
    }
}
