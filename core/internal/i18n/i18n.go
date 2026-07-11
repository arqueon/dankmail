// Package i18n localizes the daemon's and CLI's user-visible strings.
// English is the source language and every key below is the English
// text itself; other languages are overrides, mirroring the Quickshell
// UI (Common/I18n.qml + translations/es.json, where en.json is empty
// for the same reason). The locale is resolved once at startup from
// DMAIL_LANG, then LC_ALL / LC_MESSAGES / LANG.
package i18n

import (
	"os"
	"strings"
)

var lang = detectFrom(os.Getenv)

// T returns the translation of the English source string s for the
// process locale, or s itself when no override exists.
func T(s string) string { return tFor(lang, s) }

func tFor(lang, s string) string {
	if m, ok := overrides[lang]; ok {
		if t, ok := m[s]; ok {
			return t
		}
	}
	return s
}

func detectFrom(getenv func(string) string) string {
	for _, key := range []string{"DMAIL_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(key)
		if v == "" {
			continue
		}
		// "es_MX.UTF-8" / "es-419" / "es@valencia" → "es"
		v = strings.FieldsFunc(v, func(r rune) bool {
			return r == '_' || r == '-' || r == '.' || r == '@'
		})[0]
		return strings.ToLower(v)
	}
	return "en"
}

var overrides = map[string]map[string]string{
	"es": {
		// daemon: notification action buttons
		"Mark read":   "Marcar leído",
		"Archive":     "Archivar",
		"Trash":       "Borrar",
		"Snooze":      "Posponer",
		"Open in web": "Abrir en web",

		// daemon: notification texts
		"dankmail: operation failed":                "dankmail: operación fallida",
		"dankmail: account needs re-authentication": "dankmail: cuenta requiere re-autenticación",
		"Use the key button in Settings → Accounts, or run: dmail account reauth": "Usa el botón de llave en Ajustes → Cuentas, o ejecuta: dmail account reauth",
		"Snoozed thread woke up":                    "Pospuesto despertó",

		// oauth: browser callback page
		"Authorization denied. You can close this tab.":                      "Autorización denegada. Puedes cerrar esta pestaña.",
		"Account authorized. You can close this tab and return to dankmail.": "Cuenta autorizada. Puedes cerrar esta pestaña y volver a dankmail.",

		// cli: account management
		"Note:":                        "Nota:",
		"Password (or app password): ": "Contraseña (o app password): ",
		"Testing the IMAP connection…": "Probando conexión IMAP…",
		"Account %s added (%s). IMAP syncing arrives with the next ring; the account stays parked until then.\n": "Cuenta %s añadida (%s). La sincronización IMAP llega en el siguiente anillo; la cuenta queda en pausa.\n",
		"Account %s updated (%s).\n":                          "Cuenta %s actualizada (%s).\n",
		"Account %s added (%s).\n":                            "Cuenta %s añadida (%s).\n",
		"Account %s re-authorized (%s).\n":                    "Cuenta %s re-autorizada (%s).\n",
		"Opening the browser to authorize the account…":       "Abriendo el navegador para autorizar la cuenta…",
		"Account removed (the remote mailbox is untouched).":  "Cuenta eliminada (la bandeja remota queda intacta).",
		"Account re-authenticated.":                           "Cuenta re-autenticada.",
		"Daemon reloaded.":                                    "Daemon recargado.",
		"(Daemon not running; changes apply when it starts.)": "(Daemon no activo; los cambios aplican al arrancarlo.)",
	},
}
