import QtQuick
import qs.Common
import qs.Modules.Plugins
import qs.Widgets

PluginSettings {
    id: root
    pluginId: "dankmailUnread"

    StyledText {
        width: parent.width
        text: I18n.trFor("dankmailUnread", "Dankmail Unread")
        font.pixelSize: Theme.fontSizeLarge
        font.weight: Font.Medium
        color: Theme.surfaceText
    }

    StyledText {
        width: parent.width
        text: I18n.trFor("dankmailUnread", "Live unread badge and triage popout for Dankmail. Left click opens the popout, middle click toggles the app, and right click syncs.")
        font.pixelSize: Theme.fontSizeSmall
        color: Theme.surfaceVariantText
        wrapMode: Text.WordWrap
    }

    ToggleSetting {
        settingKey: "hideWhenZero"
        label: I18n.trFor("dankmailUnread", "Hide when inbox is clear")
        description: I18n.trFor("dankmailUnread", "Collapse the pill while there is no unread mail")
        defaultValue: false
    }

    ToggleSetting {
        settingKey: "showDndDot"
        label: I18n.trFor("dankmailUnread", "Do-not-disturb indicator")
        description: I18n.trFor("dankmailUnread", "Show a small dot while Dankmail's DND mode is active")
        defaultValue: true
    }
}
