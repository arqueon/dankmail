import QtQuick
import qs.Common
import qs.Modules.Plugins
import qs.Widgets

PluginSettings {
    id: root
    pluginId: "dankmailUnread"

    StyledText {
        width: parent.width
        text: "Dank Mail Unread"
        font.pixelSize: Theme.fontSizeLarge
        font.weight: Font.Bold
        color: Theme.surfaceText
    }

    StyledText {
        width: parent.width
        text: "Shows the unread count from the dankmail daemon. Left click opens the triage popout; right click toggles the dankmail window."
        font.pixelSize: Theme.fontSizeSmall
        color: Theme.surfaceVariantText
        wrapMode: Text.WordWrap
    }

    StringSetting {
        settingKey: "dmailBin"
        label: "dmail binary"
        description: "Path to the dmail binary (leave default if installed with PREFIX=~/.local)."
        defaultValue: ""
        placeholder: "~/.local/bin/dmail"
    }

    SliderSetting {
        settingKey: "maxThreads"
        label: "Popout threads"
        description: "Maximum unread threads listed in the popout."
        defaultValue: 10
        minimum: 3
        maximum: 30
        unit: ""
        leftIcon: "mail"
    }
}
