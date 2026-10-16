import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets

// Native DankMail file picker modal styled with Theme.* (avoids the
// broken QtQuick.Dialogs Material fallback when QT_QUICK_CONTROLS_STYLE=Material).
FloatingWindow {
    id: picker

    title: I18n.tr("Attach files", "compose")
    implicitWidth: 760
    implicitHeight: 540
    minimumSize: Qt.size(620, 420)
    color: Theme.surface
    visible: false

    signal filesSelected(var paths)

    property string currentPath: ""
    property string parentPath: ""
    property bool showHidden: false
    property string filterText: ""
    property var places: []
    property var entries: []
    property var selectedPaths: []
    property string errorText: ""

    readonly property var filteredEntries: {
        const q = filterText.trim().toLowerCase();
        if (q === "")
            return entries;
        return entries.filter(e => String(e.name || "").toLowerCase().indexOf(q) !== -1);
    }

    function open() {
        selectedPaths = [];
        filterText = "";
        filterField.text = "";
        errorText = "";
        visible = true;
        navigate(currentPath);
    }

    function navigate(targetPath) {
        errorText = "";
        DankMailService.listDirectory(targetPath, showHidden, resp => {
            if (!resp || resp.error) {
                picker.errorText = resp && resp.error ? String(resp.error) : "error";
                return;
            }
            const res = resp.result || {};
            picker.currentPath = res.path || "";
            picker.parentPath = res.parent || "";
            picker.places = res.places || [];
            picker.entries = res.entries || [];
            pathField.text = picker.currentPath;
        });
    }

    function toggleSelect(filePath) {
        const list = selectedPaths.slice();
        const idx = list.indexOf(filePath);
        if (idx === -1)
            list.push(filePath);
        else
            list.splice(idx, 1);
        selectedPaths = list;
    }

    function confirmSelection() {
        if (selectedPaths.length === 0)
            return;
        const out = selectedPaths.slice();
        visible = false;
        filesSelected(out);
    }

    function placeLabel(key) {
        switch (key) {
        case "home":
            return I18n.tr("Home", "file picker");
        case "downloads":
            return I18n.tr("Downloads", "file picker");
        case "documents":
            return I18n.tr("Documents", "file picker");
        case "desktop":
            return I18n.tr("Desktop", "file picker");
        case "pictures":
            return I18n.tr("Pictures", "file picker");
        case "nextcloud":
            return "Nextcloud";
        default:
            return key;
        }
    }

    function iconForEntry(entry) {
        if (entry.isDir)
            return "folder";
        const name = String(entry.name || "").toLowerCase();
        if (/\.(png|jpe?g|gif|webp|svg|bmp)$/.test(name))
            return "image";
        if (/\.pdf$/.test(name))
            return "picture_as_pdf";
        if (/\.(zip|tar|gz|xz|zst|7z|rar)$/.test(name))
            return "folder_zip";
        if (/\.(md|txt|doc|docx|odt|rtf|csv|xlsx|pptx)$/.test(name))
            return "description";
        return "insert_drive_file";
    }

    function formatSize(bytes) {
        if (!bytes || bytes <= 0)
            return "0 B";
        if (bytes < 1024)
            return bytes + " B";
        if (bytes < 1024 * 1024)
            return (bytes / 1024).toFixed(1) + " KB";
        return (bytes / (1024 * 1024)).toFixed(1) + " MB";
    }

    function formatDate(iso) {
        if (!iso)
            return "";
        const d = new Date(iso);
        return Qt.formatDateTime(d, "dd/MM/yy HH:mm");
    }

    Shortcut {
        sequence: "Escape"
        onActivated: picker.visible = false
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header + Path bar.
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 54
            color: Theme.surfaceContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.spacingM
                anchors.rightMargin: Theme.spacingM
                spacing: Theme.spacingS

                DankActionButton {
                    iconName: "arrow_upward"
                    enabled: picker.parentPath !== "" && picker.parentPath !== picker.currentPath
                    onClicked: picker.navigate(picker.parentPath)
                }

                DankTextField {
                    id: pathField
                    Layout.fillWidth: true
                    Layout.preferredHeight: 36
                    iconName: "folder_open"
                    placeholderText: "/home/…"
                    onAccepted: picker.navigate(text.trim())
                }

                DankTextField {
                    id: filterField
                    Layout.preferredWidth: 190
                    Layout.preferredHeight: 36
                    iconName: "search"
                    placeholderText: I18n.tr("Filter files…", "file picker")
                    onTextChanged: picker.filterText = text
                }

                DankActionButton {
                    iconName: picker.showHidden ? "visibility" : "visibility_off"
                    iconColor: picker.showHidden ? Theme.primary : Theme.surfaceTextMedium
                    onClicked: {
                        picker.showHidden = !picker.showHidden;
                        picker.navigate(picker.currentPath);
                    }
                }

                DankActionButton {
                    iconName: "close"
                    onClicked: picker.visible = false
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        // Body: Places sidebar + Directory entries list.
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Left sidebar: Places.
            Rectangle {
                Layout.preferredWidth: 176
                Layout.fillHeight: true
                color: Theme.surfaceContainer

                Column {
                    anchors.fill: parent
                    anchors.margins: Theme.spacingS
                    spacing: 4

                    Repeater {
                        model: picker.places

                        delegate: StyledRect {
                            id: placeItem
                            required property var modelData
                            readonly property bool active: picker.currentPath === modelData.path
                            width: parent.width
                            height: 36
                            radius: Theme.cornerRadiusSmall
                            color: active ? Theme.primaryContainer : "transparent"

                            Row {
                                anchors.left: parent.left
                                anchors.leftMargin: Theme.spacingS
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.spacingS

                                DankIcon {
                                    name: placeItem.modelData.icon || "folder"
                                    size: 18
                                    color: placeItem.active ? Theme.primary : Theme.surfaceTextMedium
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                StyledText {
                                    text: picker.placeLabel(placeItem.modelData.key)
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: placeItem.active ? Font.DemiBold : Font.Normal
                                    color: placeItem.active ? Theme.primary : Theme.surfaceText
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: picker.navigate(placeItem.modelData.path)
                            }
                        }
                    }
                }
            }

            Rectangle {
                Layout.preferredWidth: 1
                Layout.fillHeight: true
                color: Theme.outlineMedium
            }

            // Right pane: File and folder list.
            Item {
                Layout.fillWidth: true
                Layout.fillHeight: true

                ListView {
                    id: fileList
                    anchors.fill: parent
                    anchors.margins: Theme.spacingXS
                    clip: true
                    model: picker.filteredEntries
                    boundsBehavior: Flickable.StopAtBounds
                    spacing: 2

                    delegate: StyledRect {
                        id: entryRow
                        required property var modelData
                        readonly property bool checked: !modelData.isDir && picker.selectedPaths.indexOf(modelData.path) !== -1

                        width: fileList.width
                        height: 40
                        radius: Theme.cornerRadiusSmall
                        color: checked ? Theme.surfaceSelected : (entryHover.hovered ? Theme.surfaceContainerHigh : "transparent")

                        HoverHandler {
                            id: entryHover
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.spacingM
                            anchors.rightMargin: Theme.spacingM
                            spacing: Theme.spacingS

                            Rectangle {
                                visible: !entryRow.modelData.isDir
                                width: 18
                                height: 18
                                radius: 4
                                color: entryRow.checked ? Theme.primary : "transparent"
                                border.width: 2
                                border.color: entryRow.checked ? Theme.primary : Theme.outlineMedium

                                DankIcon {
                                    anchors.centerIn: parent
                                    visible: entryRow.checked
                                    name: "check"
                                    size: 12
                                    color: Theme.surface
                                }
                            }

                            DankIcon {
                                name: picker.iconForEntry(entryRow.modelData)
                                size: 20
                                color: entryRow.modelData.isDir ? Theme.primary : Theme.surfaceTextMedium
                            }

                            StyledText {
                                Layout.fillWidth: true
                                text: entryRow.modelData.name
                                font.pixelSize: Theme.fontSizeSmall
                                font.weight: entryRow.modelData.isDir ? Font.Medium : Font.Normal
                                color: Theme.surfaceText
                                elide: Text.ElideMiddle
                                maximumLineCount: 1
                            }

                            StyledText {
                                visible: !entryRow.modelData.isDir
                                text: picker.formatSize(entryRow.modelData.size)
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.surfaceTextMedium
                            }

                            StyledText {
                                text: picker.formatDate(entryRow.modelData.modTime)
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.surfaceTextAlpha
                            }
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            onClicked: {
                                if (entryRow.modelData.isDir)
                                    picker.navigate(entryRow.modelData.path);
                                else
                                    picker.toggleSelect(entryRow.modelData.path);
                            }
                        }
                    }
                }

                StyledText {
                    anchors.centerIn: parent
                    visible: picker.filteredEntries.length === 0
                    text: picker.errorText !== "" ? picker.errorText : I18n.tr("Empty folder", "file picker")
                    color: picker.errorText !== "" ? Theme.error : Theme.surfaceTextMedium
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        // Footer bar: selection count + Cancel / Attach.
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 52
            color: Theme.surfaceContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.spacingL
                anchors.rightMargin: Theme.spacingL
                spacing: Theme.spacingM

                StyledText {
                    Layout.fillWidth: true
                    text: picker.selectedPaths.length > 0 ? I18n.tr("%1 selected", "bulk action").arg(picker.selectedPaths.length) : I18n.tr("Click files to select one or more attachments", "file picker")
                    font.pixelSize: Theme.fontSizeSmall
                    color: picker.selectedPaths.length > 0 ? Theme.primary : Theme.surfaceTextMedium
                    elide: Text.ElideRight
                }

                StyledRect {
                    implicitWidth: Math.ceil(cancelBtnLabel.contentWidth) + Theme.spacingL * 2
                    implicitHeight: 34
                    Layout.preferredWidth: implicitWidth
                    Layout.preferredHeight: 34
                    radius: 17
                    color: Theme.surfaceContainerHigh

                    StyledText {
                        id: cancelBtnLabel
                        anchors.centerIn: parent
                        text: I18n.tr("Cancel", "account wizard nav")
                        wrapMode: Text.NoWrap
                        elide: Text.ElideNone
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceText
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: picker.visible = false
                    }
                }

                StyledRect {
                    readonly property bool ready: picker.selectedPaths.length > 0
                    implicitWidth: Math.ceil(attachBtnLabel.contentWidth) + Theme.spacingXL * 2
                    implicitHeight: 34
                    Layout.preferredWidth: implicitWidth
                    Layout.preferredHeight: 34
                    radius: 17
                    color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                    opacity: ready ? 1 : 0.6

                    StyledText {
                        id: attachBtnLabel
                        anchors.centerIn: parent
                        text: I18n.tr("Attach", "file picker")
                        wrapMode: Text.NoWrap
                        elide: Text.ElideNone
                        font.pixelSize: Theme.fontSizeSmall
                        font.weight: Font.DemiBold
                        color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                    }

                    StateLayer {
                        disabled: !parent.ready
                        stateColor: Theme.primary
                        onClicked: picker.confirmSelection()
                    }
                }
            }
        }
    }
}
