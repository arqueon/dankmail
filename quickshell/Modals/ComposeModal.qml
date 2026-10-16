import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets
import "../Common/BodyFormatter.js" as BodyFormatter

// Full composer and draft editor: sender-account selector, To/Cc/Bcc with
// contact autocomplete, Subject, formatting + link insertion toolbar,
// live preview toggle, file attachments, draft saving/editing, and send.
FloatingWindow {
    id: modal

    title: draftId !== "" ? I18n.tr("Edit draft", "compose") : I18n.tr("New message", "compose")
    implicitWidth: 720
    implicitHeight: 680
    minimumSize: Qt.size(580, 520)
    color: Theme.surface
    visible: false

    property string draftId: ""
    property int threadId: 0
    property string providerThreadId: ""
    property string inReplyTo: ""
    property string accountId: ""
    property var recipients: []
    property var ccRecipients: []
    property var bccRecipients: []
    property bool showCcBcc: false
    property string activeRecipientField: "to"
    property var suggestions: []
    property var attachments: []
    property bool previewMode: false
    property bool showLinkBar: false
    property bool sending: false
    property bool savingDraft: false
    property string error: ""
    property string statusNotice: ""

    function resetFields() {
        draftId = "";
        threadId = 0;
        providerThreadId = "";
        inReplyTo = "";
        accountId = DankMailService.accounts.length > 0 ? DankMailService.accounts[0].id : "";
        recipients = [];
        ccRecipients = [];
        bccRecipients = [];
        showCcBcc = false;
        activeRecipientField = "to";
        suggestions = [];
        attachments = [];
        previewMode = false;
        showLinkBar = false;
        sending = false;
        savingDraft = false;
        error = "";
        statusNotice = "";
        toInput.text = "";
        ccInput.text = "";
        bccInput.text = "";
        subjectField.text = "";
        bodyInput.text = "";
        linkLabelInput.text = "";
        linkUrlInput.text = "";
    }

    function show() {
        resetFields();
        visible = true;
        toInput.forceActiveFocus();
    }

    function editDraft(d) {
        if (!d)
            return;
        resetFields();
        draftId = d.id || "";
        threadId = d.threadId || 0;
        providerThreadId = d.providerThreadId || "";
        inReplyTo = d.inReplyTo || "";
        accountId = d.accountId || (DankMailService.accounts.length > 0 ? DankMailService.accounts[0].id : "");
        recipients = (d.to || []).slice();
        ccRecipients = (d.cc || []).slice();
        bccRecipients = (d.bcc || []).slice();
        showCcBcc = ccRecipients.length > 0 || bccRecipients.length > 0;
        attachments = (d.attachments || []).map(a => ({
            "filename": a.filename || "",
            "mimeType": a.mimeType || "",
            "size": a.size || 0,
            "path": a.path || ""
        }));
        subjectField.text = d.subject || "";
        bodyInput.text = d.body || "";
        visible = true;
        bodyInput.forceActiveFocus();
    }

    function normalizeAddressToken(raw) {
        const parsed = DankMailService.parseAddress(raw);
        return parsed.email !== "" ? parsed.email : String(raw || "").trim();
    }

    function addRecipientTo(field, rawText) {
        const parts = String(rawText || "").split(/[;,]+/);
        let list = field === "cc" ? ccRecipients.slice() : field === "bcc" ? bccRecipients.slice() : recipients.slice();
        for (let i = 0; i < parts.length; i++) {
            const email = normalizeAddressToken(parts[i]);
            if (email === "" || email.indexOf("@") === -1)
                continue;
            if (list.indexOf(email) === -1)
                list.push(email);
        }
        if (field === "cc") {
            ccRecipients = list;
            ccInput.text = "";
        } else if (field === "bcc") {
            bccRecipients = list;
            bccInput.text = "";
        } else {
            recipients = list;
            toInput.text = "";
        }
        suggestions = [];
    }

    function addRecipient(email) {
        addRecipientTo(activeRecipientField, email);
    }

    function removeRecipientFrom(field, email) {
        if (field === "cc")
            ccRecipients = ccRecipients.filter(r => r !== email);
        else if (field === "bcc")
            bccRecipients = bccRecipients.filter(r => r !== email);
        else
            recipients = recipients.filter(r => r !== email);
    }

    function flushTypedRecipients() {
        if (toInput.text.trim() !== "")
            addRecipientTo("to", toInput.text);
        if (ccInput.text.trim() !== "")
            addRecipientTo("cc", ccInput.text);
        if (bccInput.text.trim() !== "")
            addRecipientTo("bcc", bccInput.text);
    }

    function addAttachmentFiles(fileUrls) {
        const list = attachments.slice();
        for (let i = 0; i < fileUrls.length; i++) {
            let p = String(fileUrls[i] || "").trim();
            if (p.startsWith("file://")) {
                try {
                    p = decodeURIComponent(p.substring(7));
                } catch (e) {
                    p = p.substring(7);
                }
            }
            if (p === "")
                continue;
            const slash = p.lastIndexOf("/");
            const name = slash >= 0 ? p.substring(slash + 1) : p;
            if (!list.some(a => a.path === p && a.filename === name)) {
                list.push({
                    "filename": name,
                    "path": p,
                    "size": 0,
                    "mimeType": ""
                });
            }
        }
        attachments = list;
    }

    function removeAttachment(idx) {
        const list = attachments.slice();
        list.splice(idx, 1);
        attachments = list;
    }

    function formatSize(bytes) {
        if (!bytes || bytes <= 0)
            return "";
        if (bytes < 1024)
            return bytes + " B";
        if (bytes < 1024 * 1024)
            return (bytes / 1024).toFixed(0) + " KB";
        return (bytes / (1024 * 1024)).toFixed(1) + " MB";
    }

    function wrapSelection(prefix, suffix, placeholder) {
        const start = bodyInput.selectionStart;
        const end = bodyInput.selectionEnd;
        const full = bodyInput.text;
        const selected = start !== end ? full.substring(start, end) : placeholder;
        const replacement = prefix + selected + suffix;
        bodyInput.text = full.substring(0, start) + replacement + full.substring(end);
        bodyInput.cursorPosition = start + replacement.length;
        bodyInput.forceActiveFocus();
    }

    function insertLinePrefix(prefix) {
        const pos = bodyInput.cursorPosition;
        const full = bodyInput.text;
        const lineStart = full.lastIndexOf("\n", Math.max(0, pos - 1)) + 1;
        bodyInput.text = full.substring(0, lineStart) + prefix + full.substring(lineStart);
        bodyInput.cursorPosition = pos + prefix.length;
        bodyInput.forceActiveFocus();
    }

    function openLinkBar() {
        showLinkBar = !showLinkBar;
        if (showLinkBar) {
            const start = bodyInput.selectionStart;
            const end = bodyInput.selectionEnd;
            if (start !== end)
                linkLabelInput.text = bodyInput.text.substring(start, end);
            linkUrlInput.forceActiveFocus();
        }
    }

    function insertMarkdownLink() {
        let url = linkUrlInput.text.trim();
        if (url === "")
            return;
        if (!/^(https?:\/\/|mailto:)/i.test(url)) {
            if (url.indexOf("@") !== -1 && url.indexOf("/") === -1)
                url = "mailto:" + url;
            else
                url = "https://" + url;
        }
        const label = linkLabelInput.text.trim() !== "" ? linkLabelInput.text.trim() : url;
        const md = "[" + label + "](" + url + ")";
        const start = bodyInput.selectionStart;
        const end = bodyInput.selectionEnd;
        const full = bodyInput.text;
        bodyInput.text = full.substring(0, start) + md + full.substring(end);
        bodyInput.cursorPosition = start + md.length;
        linkLabelInput.text = "";
        linkUrlInput.text = "";
        showLinkBar = false;
        bodyInput.forceActiveFocus();
    }

    function saveDraft(closeAfter) {
        flushTypedRecipients();
        if (savingDraft || accountId === "")
            return;
        if (recipients.length === 0 && ccRecipients.length === 0 && bccRecipients.length === 0 && subjectField.text.trim() === "" && bodyInput.text.trim() === "" && attachments.length === 0)
            return;
        savingDraft = true;
        error = "";
        statusNotice = "";
        DankMailService.saveDraft({
            "id": draftId,
            "account": accountId,
            "threadId": threadId,
            "providerThreadId": providerThreadId,
            "inReplyTo": inReplyTo,
            "to": recipients,
            "cc": ccRecipients,
            "bcc": bccRecipients,
            "subject": subjectField.text,
            "body": bodyInput.text,
            "attachments": attachments
        }, resp => {
            modal.savingDraft = false;
            if (resp.error) {
                modal.error = resp.error;
                return;
            }
            if (resp.result && resp.result.id)
                modal.draftId = resp.result.id;
            if (resp.result && resp.result.attachments)
                modal.attachments = resp.result.attachments;
            modal.statusNotice = I18n.tr("Draft saved", "compose");
            if (closeAfter)
                modal.visible = false;
        });
    }

    function discardDraft() {
        if (draftId !== "") {
            const idToRemove = draftId;
            DankMailService.deleteDraft(idToRemove, () => {
                modal.visible = false;
            });
        } else {
            modal.visible = false;
        }
    }

    function send() {
        flushTypedRecipients();
        const totalRcpts = recipients.length + ccRecipients.length + bccRecipients.length;
        if (totalRcpts === 0 || (bodyInput.text.trim() === "" && attachments.length === 0) || sending || accountId === "")
            return;
        sending = true;
        error = "";
        statusNotice = "";
        DankMailService.compose(accountId, recipients, subjectField.text, bodyInput.text, resp => {
            modal.sending = false;
            if (resp.error) {
                modal.error = resp.error;
                return;
            }
            modal.visible = false;
        }, {
            "cc": ccRecipients,
            "bcc": bccRecipients,
            "attachments": attachments,
            "draftId": draftId
        });
    }

    FilePickerModal {
        id: filePicker
        onFilesSelected: paths => modal.addAttachmentFiles(paths)
    }

    Shortcut {
        sequence: "Ctrl+S"
        onActivated: modal.saveDraft(false)
    }

    Shortcut {
        sequence: "Ctrl+B"
        onActivated: modal.wrapSelection("**", "**", I18n.tr("bold", "compose"))
    }

    Shortcut {
        sequence: "Ctrl+I"
        onActivated: modal.wrapSelection("*", "*", I18n.tr("italic", "compose"))
    }

    Shortcut {
        sequence: "Ctrl+K"
        onActivated: modal.openLinkBar()
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.spacingXL
        spacing: Theme.spacingS

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingM

            DankIcon {
                name: modal.draftId !== "" ? "edit_note" : "edit_square"
                size: Theme.iconSize
                color: Theme.primary
            }

            StyledText {
                Layout.fillWidth: true
                text: modal.draftId !== "" ? I18n.tr("Edit draft", "compose") : I18n.tr("New message", "compose")
                font.pixelSize: Theme.fontSizeLarge
                font.weight: Font.DemiBold
            }

            StyledText {
                visible: modal.statusNotice !== ""
                text: modal.statusNotice
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.primary
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

        // To recipients chips.
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
                                onClicked: modal.removeRecipientFrom("to", rcptChip.modelData)
                            }
                        }
                    }
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingS

            DankTextField {
                id: toInput
                Layout.fillWidth: true
                iconName: "person"
                placeholderText: I18n.tr("To — type a name or address", "compose")
                onTextChanged: {
                    modal.activeRecipientField = "to";
                    suggestDebounce.restart();
                }
                onAccepted: modal.addRecipientTo("to", text)
            }

            StyledRect {
                implicitWidth: Math.ceil(ccBccLabel.contentWidth) + Theme.spacingM * 2
                implicitHeight: 30
                Layout.preferredWidth: implicitWidth
                Layout.preferredHeight: 30
                radius: 15
                color: modal.showCcBcc ? Theme.primaryContainer : Theme.surfaceContainerHigh

                StyledText {
                    id: ccBccLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Cc / Bcc", "compose")
                    wrapMode: Text.NoWrap
                    elide: Text.ElideNone
                    font.pixelSize: Theme.fontSizeSmall
                    color: modal.showCcBcc ? Theme.primary : Theme.surfaceTextMedium
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: modal.showCcBcc = !modal.showCcBcc
                }
            }
        }

        // CC chips + input.
        Flow {
            Layout.fillWidth: true
            spacing: Theme.spacingXS
            visible: modal.showCcBcc && modal.ccRecipients.length > 0

            Repeater {
                model: modal.ccRecipients

                delegate: StyledRect {
                    id: ccChip
                    required property string modelData
                    width: ccRow.implicitWidth + Theme.spacingL
                    height: 26
                    radius: 13
                    color: Theme.surfaceContainerHighest

                    RowLayout {
                        id: ccRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingXS

                        StyledText {
                            text: "Cc: " + ccChip.modelData
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
                                onClicked: modal.removeRecipientFrom("cc", ccChip.modelData)
                            }
                        }
                    }
                }
            }
        }

        DankTextField {
            id: ccInput
            visible: modal.showCcBcc
            Layout.fillWidth: true
            iconName: "group"
            placeholderText: I18n.tr("Cc — carbon copy recipients", "compose")
            onTextChanged: {
                modal.activeRecipientField = "cc";
                suggestDebounce.restart();
            }
            onAccepted: modal.addRecipientTo("cc", text)
        }

        // BCC chips + input.
        Flow {
            Layout.fillWidth: true
            spacing: Theme.spacingXS
            visible: modal.showCcBcc && modal.bccRecipients.length > 0

            Repeater {
                model: modal.bccRecipients

                delegate: StyledRect {
                    id: bccChip
                    required property string modelData
                    width: bccRow.implicitWidth + Theme.spacingL
                    height: 26
                    radius: 13
                    color: Theme.surfaceContainerHighest

                    RowLayout {
                        id: bccRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingXS

                        StyledText {
                            text: "Bcc: " + bccChip.modelData
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
                                onClicked: modal.removeRecipientFrom("bcc", bccChip.modelData)
                            }
                        }
                    }
                }
            }
        }

        DankTextField {
            id: bccInput
            visible: modal.showCcBcc
            Layout.fillWidth: true
            iconName: "visibility_off"
            placeholderText: I18n.tr("Bcc — blind carbon copy recipients", "compose")
            onTextChanged: {
                modal.activeRecipientField = "bcc";
                suggestDebounce.restart();
            }
            onAccepted: modal.addRecipientTo("bcc", text)
        }

        Timer {
            id: suggestDebounce
            interval: 200
            repeat: false
            onTriggered: {
                const activeText = modal.activeRecipientField === "cc" ? ccInput.text : modal.activeRecipientField === "bcc" ? bccInput.text : toInput.text;
                const q = activeText.trim();
                if (q === "") {
                    modal.suggestions = [];
                    return;
                }
                DankMailService.searchContacts(q, modal.accountId, list => modal.suggestions = list);
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
                    implicitHeight: 34
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

        // Formatting, Link, and Attachment Toolbar.
        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingXS

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "format_bold"
                onClicked: modal.wrapSelection("**", "**", I18n.tr("bold", "compose"))
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "format_italic"
                onClicked: modal.wrapSelection("*", "*", I18n.tr("italic", "compose"))
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "add_link"
                iconColor: modal.showLinkBar ? Theme.primary : Theme.surfaceText
                onClicked: modal.openLinkBar()
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "format_list_bulleted"
                onClicked: modal.insertLinePrefix("- ")
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "format_list_numbered"
                onClicked: modal.insertLinePrefix("1. ")
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "format_quote"
                onClicked: modal.insertLinePrefix("> ")
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "code"
                onClicked: modal.wrapSelection("`", "`", "code")
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "horizontal_rule"
                onClicked: modal.wrapSelection("\n---\n", "", "")
            }

            Rectangle {
                width: 1
                height: 18
                color: Theme.outlineLight
            }

            DankActionButton {
                buttonSize: 30
                iconSize: 18
                iconName: "attach_file"
                iconColor: modal.attachments.length > 0 ? Theme.primary : Theme.surfaceText
                onClicked: filePicker.open()
            }

            Item {
                Layout.fillWidth: true
            }

            StyledRect {
                implicitWidth: Math.ceil(previewToggleLabel.contentWidth) + Theme.spacingM * 2
                implicitHeight: 28
                Layout.preferredWidth: implicitWidth
                Layout.preferredHeight: 28
                radius: 14
                color: modal.previewMode ? Theme.primaryContainer : Theme.surfaceContainerHigh

                StyledText {
                    id: previewToggleLabel
                    anchors.centerIn: parent
                    text: modal.previewMode ? I18n.tr("Edit text", "compose") : I18n.tr("Preview", "compose")
                    wrapMode: Text.NoWrap
                    elide: Text.ElideNone
                    font.pixelSize: Theme.fontSizeSmall
                    color: modal.previewMode ? Theme.primary : Theme.surfaceTextMedium
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: modal.previewMode = !modal.previewMode
                }
            }
        }

        // Inline URL link insertion bar.
        StyledRect {
            visible: modal.showLinkBar
            Layout.fillWidth: true
            implicitHeight: linkRow.implicitHeight + Theme.spacingS * 2
            radius: Theme.cornerRadiusSmall
            color: Theme.surfaceContainerHigh

            RowLayout {
                id: linkRow
                anchors.fill: parent
                anchors.margins: Theme.spacingS
                spacing: Theme.spacingS

                DankTextField {
                    id: linkLabelInput
                    Layout.preferredWidth: 180
                    Layout.preferredHeight: 34
                    iconName: "label"
                    placeholderText: I18n.tr("Link text", "compose")
                    onAccepted: linkUrlInput.forceActiveFocus()
                }

                DankTextField {
                    id: linkUrlInput
                    Layout.fillWidth: true
                    Layout.preferredHeight: 34
                    iconName: "link"
                    placeholderText: I18n.tr("https://example.org", "compose")
                    onAccepted: modal.insertMarkdownLink()
                }

                StyledRect {
                    implicitWidth: Math.ceil(insertLinkText.contentWidth) + Theme.spacingM * 2
                    implicitHeight: 32
                    Layout.preferredWidth: implicitWidth
                    Layout.preferredHeight: 32
                    radius: 16
                    color: Theme.primaryContainer

                    StyledText {
                        id: insertLinkText
                        anchors.centerIn: parent
                        text: I18n.tr("Insert link", "compose")
                        wrapMode: Text.NoWrap
                        elide: Text.ElideNone
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.primary
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: modal.insertMarkdownLink()
                    }
                }
            }
        }

        // Attachments chips.
        Flow {
            Layout.fillWidth: true
            spacing: Theme.spacingXS
            visible: modal.attachments.length > 0

            Repeater {
                model: modal.attachments

                delegate: StyledRect {
                    id: attChip
                    required property var modelData
                    required property int index
                    width: attRow.implicitWidth + Theme.spacingL
                    height: 28
                    radius: 14
                    color: Theme.surfaceContainerHigh
                    border.width: 1
                    border.color: Theme.outlineMedium

                    RowLayout {
                        id: attRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingXS

                        DankIcon {
                            name: "attach_file"
                            size: 14
                            color: Theme.primary
                        }

                        StyledText {
                            text: attChip.modelData.filename + (modal.formatSize(attChip.modelData.size) !== "" ? " · " + modal.formatSize(attChip.modelData.size) : "")
                            font.pixelSize: Theme.fontSizeSmall
                            maximumLineCount: 1
                        }

                        DankIcon {
                            name: "close"
                            size: 14
                            color: Theme.surfaceTextMedium

                            MouseArea {
                                anchors.fill: parent
                                anchors.margins: -4
                                cursorShape: Qt.PointingHandCursor
                                onClicked: modal.removeAttachment(attChip.index)
                            }
                        }
                    }
                }
            }
        }

        // Editor / Rich Preview pane.
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
                contentHeight: modal.previewMode ? bodyPreview.implicitHeight : Math.max(bodyInput.implicitHeight, parent.height - Theme.spacingS * 2)
                clip: true

                TextEdit {
                    id: bodyInput
                    visible: !modal.previewMode
                    width: parent.width
                    height: Math.max(implicitHeight, parent.height)
                    wrapMode: TextEdit.Wrap
                    textFormat: TextEdit.PlainText
                    selectByMouse: true
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

                TextEdit {
                    id: bodyPreview
                    visible: modal.previewMode
                    width: parent.width
                    readOnly: true
                    selectByMouse: true
                    wrapMode: TextEdit.Wrap
                    textFormat: TextEdit.RichText
                    color: Theme.surfaceText
                    selectionColor: Theme.primarySelected
                    font.family: Theme.fontFamily
                    font.pixelSize: Theme.fontSizeMedium
                    text: BodyFormatter.format(bodyInput.text, {
                        linkColor: String(Theme.primary),
                        quoteColor: String(Theme.surfaceTextMedium),
                        linkLabel: I18n.tr("Link", "message body")
                    })
                    onLinkActivated: link => {
                        const clean = BodyFormatter.unwrapUrl ? BodyFormatter.unwrapUrl(link) : link;
                        if (BodyFormatter.safeUrl(clean))
                            DankMailService.openExternalUrl(clean);
                    }

                    HoverHandler {
                        cursorShape: bodyPreview.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.IBeamCursor
                    }
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingS

            StyledRect {
                visible: modal.draftId !== ""
                implicitWidth: Math.ceil(discardLabel.contentWidth) + Theme.spacingL * 2
                implicitHeight: 36
                Layout.preferredWidth: implicitWidth
                Layout.preferredHeight: 36
                radius: 18
                color: Theme.withAlpha(Theme.error, 0.14)

                StyledText {
                    id: discardLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Discard draft", "compose")
                    wrapMode: Text.NoWrap
                    elide: Text.ElideNone
                    font.pixelSize: Theme.fontSizeSmall
                    color: Theme.error
                }

                StateLayer {
                    stateColor: Theme.error
                    onClicked: modal.discardDraft()
                }
            }

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
                readonly property bool canSave: !modal.savingDraft && modal.accountId !== "" && (modal.recipients.length > 0 || modal.ccRecipients.length > 0 || modal.bccRecipients.length > 0 || subjectField.text.trim() !== "" || bodyInput.text.trim() !== "" || modal.attachments.length > 0)
                implicitWidth: Math.ceil(saveDraftLabel.contentWidth) + Theme.spacingL * 2
                implicitHeight: 36
                Layout.preferredWidth: implicitWidth
                Layout.preferredHeight: 36
                radius: 18
                color: Theme.surfaceContainerHigh
                opacity: canSave ? 1 : 0.6

                StyledText {
                    id: saveDraftLabel
                    anchors.centerIn: parent
                    text: modal.savingDraft ? I18n.tr("Saving…", "compose") : I18n.tr("Save draft", "compose")
                    wrapMode: Text.NoWrap
                    elide: Text.ElideNone
                    font.pixelSize: Theme.fontSizeSmall
                    color: parent.canSave ? Theme.surfaceText : Theme.surfaceTextMedium
                }

                StateLayer {
                    disabled: !parent.canSave
                    stateColor: Theme.primary
                    onClicked: modal.saveDraft(false)
                }
            }

            StyledRect {
                readonly property bool ready: (modal.recipients.length + modal.ccRecipients.length + modal.bccRecipients.length + (toInput.text.trim() !== "" ? 1 : 0) + (ccInput.text.trim() !== "" ? 1 : 0) + (bccInput.text.trim() !== "" ? 1 : 0)) > 0 && (bodyInput.text.trim() !== "" || modal.attachments.length > 0) && !modal.sending
                implicitWidth: Math.ceil(sendComposeLabel.contentWidth) + Theme.spacingXL * 2
                implicitHeight: 36
                Layout.preferredWidth: implicitWidth
                Layout.preferredHeight: 36
                radius: 18
                color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                opacity: ready ? 1 : 0.6

                StyledText {
                    id: sendComposeLabel
                    anchors.centerIn: parent
                    text: modal.sending ? I18n.tr("Sending…", "quick reply") : I18n.tr("Send", "quick reply")
                    wrapMode: Text.NoWrap
                    elide: Text.ElideNone
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
