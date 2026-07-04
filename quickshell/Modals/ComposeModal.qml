import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets

// Minimal compose (spec §7): sender-account selector, To with contact
// autocomplete (mail correspondents + Google contacts), Subject, plain
// body. Nothing else — no CC/BCC, no attachments. Ctrl+Enter sends.
FloatingWindow {
    id: modal

    title: I18n.tr("New message", "compose")
    implicitWidth: 640
    implicitHeight: 600
    minimumSize: Qt.size(560, 480)
    color: Theme.surface
    visible: false

    property string accountId: ""
    property var recipients: []
    property var suggestions: []
    property bool sending: false
    property string error: ""

    function show() {
        accountId = DankMailService.accounts.length > 0 ? DankMailService.accounts[0].id : "";
        recipients = [];
        suggestions = [];
        sending = false;
        error = "";
        toInput.text = "";
        subjectField.text = "";
        bodyInput.text = "";
        visible = true;
        toInput.forceActiveFocus();
    }

    function addRecipient(email) {
        email = email.trim();
        if (email === "" || email.indexOf("@") === -1)
            return;
        const list = recipients.slice();
        if (list.indexOf(email) === -1)
            list.push(email);
        recipients = list;
        toInput.text = "";
        suggestions = [];
    }

    function removeRecipient(email) {
        recipients = recipients.filter(r => r !== email);
    }

    function send() {
        // A half-typed address in the field counts.
        if (toInput.text.trim() !== "")
            addRecipient(toInput.text);
        if (recipients.length === 0 || bodyInput.text.trim() === "" || sending || accountId === "")
            return;
        sending = true;
        error = "";
        DankMailService.compose(accountId, recipients, subjectField.text, bodyInput.text, resp => {
            modal.sending = false;
            if (resp.error) {
                modal.error = resp.error;
                return;
            }
            modal.visible = false;
        });
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.spacingXL
        spacing: Theme.spacingM

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingM

            DankIcon {
                name: "edit_square"
                size: Theme.iconSize
                color: Theme.primary
            }

            StyledText {
                Layout.fillWidth: true
                text: I18n.tr("New message", "compose")
                font.pixelSize: Theme.fontSizeLarge
                font.weight: Font.DemiBold
            }

            DankActionButton {
                iconName: "close"
                onClicked: modal.visible = false
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        // Sender account.
        Flow {
            Layout.fillWidth: true
            spacing: Theme.spacingS
            visible: DankMailService.accounts.length > 1

            Repeater {
                model: DankMailService.accounts

                delegate: StyledRect {
                    id: acctChip
                    required property var modelData
                    readonly property bool active: modal.accountId === modelData.id

                    width: acctRow.implicitWidth + Theme.spacingL
                    height: 30
                    radius: 15
                    color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                    RowLayout {
                        id: acctRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingXS

                        Rectangle {
                            width: 8
                            height: 8
                            radius: 4
                            color: DankMailService.accountColor(acctChip.modelData.id)
                        }

                        StyledText {
                            text: acctChip.modelData.email
                            font.pixelSize: Theme.fontSizeSmall
                            color: acctChip.active ? Theme.primary : Theme.surfaceTextMedium
                        }
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: modal.accountId = acctChip.modelData.id
                    }
                }
            }
        }

        // Recipients: chips + autocomplete input.
        Flow {
            Layout.fillWidth: true
            spacing: Theme.spacingXS
            visible: modal.recipients.length > 0

            Repeater {
                model: modal.recipients

                delegate: StyledRect {
                    id: rcptChip
                    required property string modelData
                    width: rcptRow.implicitWidth + Theme.spacingL
                    height: 28
                    radius: 14
                    color: Theme.surfaceContainerHighest

                    RowLayout {
                        id: rcptRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingXS

                        StyledText {
                            text: rcptChip.modelData
                            font.pixelSize: Theme.fontSizeSmall
                        }

                        DankIcon {
                            name: "close"
                            size: 14
                            color: Theme.surfaceTextMedium

                            MouseArea {
                                anchors.fill: parent
                                anchors.margins: -4
                                cursorShape: Qt.PointingHandCursor
                                onClicked: modal.removeRecipient(rcptChip.modelData)
                            }
                        }
                    }
                }
            }
        }

        DankTextField {
            id: toInput
            Layout.fillWidth: true
            iconName: "person"
            placeholderText: I18n.tr("To — type a name or address", "compose")
            onTextChanged: suggestDebounce.restart()
            onAccepted: modal.addRecipient(text)

            Timer {
                id: suggestDebounce
                interval: 200
                repeat: false
                onTriggered: {
                    const q = toInput.text.trim();
                    if (q === "") {
                        modal.suggestions = [];
                        return;
                    }
                    DankMailService.searchContacts(q, modal.accountId, list => modal.suggestions = list);
                }
            }
        }

        // Suggestion list.
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 2
            visible: modal.suggestions.length > 0

            Repeater {
                model: modal.suggestions

                delegate: StyledRect {
                    id: sugRow
                    required property var modelData
                    Layout.fillWidth: true
                    implicitHeight: 36
                    radius: Theme.cornerRadiusSmall
                    color: Theme.surfaceContainer

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: Theme.spacingM
                        anchors.rightMargin: Theme.spacingM
                        spacing: Theme.spacingS

                        DankIcon {
                            name: sugRow.modelData.source === "google" ? "person" : "history"
                            size: Theme.iconSizeSmall
                            color: Theme.surfaceTextMedium
                        }

                        StyledText {
                            text: sugRow.modelData.name !== "" ? sugRow.modelData.name : sugRow.modelData.email
                            font.pixelSize: Theme.fontSizeSmall
                            font.weight: Font.Medium
                        }

                        StyledText {
                            Layout.fillWidth: true
                            text: sugRow.modelData.name !== "" ? sugRow.modelData.email : ""
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextAlpha
                            maximumLineCount: 1
                        }
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: modal.addRecipient(sugRow.modelData.email)
                    }
                }
            }
        }

        StyledText {
            Layout.fillWidth: true
            visible: DankMailService.contactsNeedReauth
            text: I18n.tr("Google contact suggestions need a re-consent: run 'dmail account reauth <id>' or re-add the account.", "compose")
            font.pixelSize: Theme.fontSizeSmall
            color: Theme.warning
            wrapMode: Text.WordWrap
        }

        DankTextField {
            id: subjectField
            Layout.fillWidth: true
            iconName: "subject"
            placeholderText: I18n.tr("Subject", "compose")
        }

        StyledRect {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: Theme.cornerRadiusSmall
            color: Theme.surfaceContainer
            border.width: 1
            border.color: bodyInput.activeFocus ? Theme.primary : Theme.outlineLight

            DankFlickable {
                anchors.fill: parent
                anchors.margins: Theme.spacingS
                contentHeight: bodyInput.implicitHeight
                clip: true

                TextEdit {
                    id: bodyInput
                    width: parent.width
                    wrapMode: TextEdit.Wrap
                    textFormat: TextEdit.PlainText
                    color: Theme.surfaceText
                    selectionColor: Theme.primarySelected
                    font.family: Theme.fontFamily
                    font.pixelSize: Theme.fontSizeMedium

                    Keys.onPressed: event => {
                        if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                            event.accepted = true;
                            modal.send();
                        }
                    }

                    StyledText {
                        visible: bodyInput.text === "" && !bodyInput.activeFocus
                        text: I18n.tr("Write your message… Ctrl+Enter sends", "compose")
                        color: Theme.surfaceTextAlpha
                    }
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingS

            StyledText {
                Layout.fillWidth: true
                text: modal.error
                visible: modal.error !== ""
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.error
                wrapMode: Text.WordWrap
            }

            Item {
                Layout.fillWidth: modal.error === ""
            }

            StyledRect {
                readonly property bool ready: modal.recipients.length + (toInput.text.trim() !== "" ? 1 : 0) > 0 && bodyInput.text.trim() !== "" && !modal.sending
                width: sendComposeLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                opacity: ready ? 1 : 0.6

                StyledText {
                    id: sendComposeLabel
                    anchors.centerIn: parent
                    text: modal.sending ? I18n.tr("Sending…", "quick reply") : I18n.tr("Send", "quick reply")
                    font.pixelSize: Theme.fontSizeSmall
                    color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                }

                StateLayer {
                    disabled: !parent.ready
                    stateColor: Theme.primary
                    onClicked: modal.send()
                }
            }
        }
    }
}
