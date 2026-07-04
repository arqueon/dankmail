# Providers roadmap (beyond Gmail)

Estado real de cada proveedor y por qué ruta entra a dankmail. La regla
de decisión: si el proveedor tiene API con sync incremental por cursor,
merece provider nativo (como Gmail); si solo habla IMAP, entra por el
provider IMAP genérico del Anillo 2.

## Ya cubiertos por el provider IMAP genérico (Anillo 2)

| Proveedor | Vía | Notas |
|---|---|---|
| iCloud Mail | IMAP + app password | Preset listo; funciona hoy. |
| Yahoo | IMAP + app password | Preset listo. |
| Fastmail | IMAP + app password | Preset listo. JMAP nativo sería anillo 3+, no urgente: IMAP les funciona perfecto. |
| **Proton Mail** | **IMAP vía Proton Mail Bridge** (127.0.0.1) | Único camino posible: el cifrado E2E impide que el servidor entregue texto plano a terceros; no hay API pública de buzón. Requiere plan de pago + Bridge corriendo. Preset listo. |
| Servidor propio / Dovecot / etc. | IMAP custom | Preset "custom". |

**Trabajo pendiente específico para Proton en el provider IMAP**: el
Bridge presenta certificado autofirmado en localhost — tanto el test de
conexión (`accounts.TestIMAP`) como el provider deben aceptar el cert
del Bridge cuando el host es loopback (opción por cuenta
`allowInsecureLocalhost`, jamás para hosts remotos). IDLE y carpetas
funcionan normal a través del Bridge.

## Microsoft (Outlook.com / Microsoft 365) — provider nativo propio

**El preset IMAP de Outlook es zona muerta a mediano plazo**: Microsoft
eliminó la autenticación básica (y está retirando los app passwords);
IMAP contra outlook.office365.com exige XOAUTH2. Como el token OAuth de
Microsoft requiere registrar una app en Azure de todos modos, hacer
IMAP+XOAUTH2 no ahorra nada frente a usar la API buena. Decisión:

**Provider `microsoft` vía Microsoft Graph API** (espejo del de Gmail):

- **Auth**: MSAL public client (Desktop), PKCE, sin client secret;
  scopes `Mail.ReadWrite` + `Mail.Send` + `offline_access`, tenant
  `common` (cubre personales y organizacionales). El broker OAuth actual
  se parametriza por endpoint (hoy está fijo a Google) — cambio pequeño.
- **Sync**: Graph **delta queries** sobre mailFolders/messages → cursor
  = deltaToken ⇒ CapHistorySync, mismo contrato que historyId.
- **Triage**: isRead / flag.flagStatus; archive = move a `archive`;
  trash = move a `deleteditems` ⇒ CapModifyFlags|CapArchive|CapTrash.
- **Deep links**: Graph devuelve `webLink` por mensaje ⇒ CapDeepLink
  nativo, mejor aún que Gmail.
- **Send**: `sendMail` con MIME (reutiliza `mailmime`) ⇒
  CapSendReply|CapCompose.
- **Setup guide**: mismo patrón servido por IPC; dankcalendar ya tiene
  el texto probado de los pasos de Azure (incluida la trampa del
  directorio/suscripción para cuentas personales) — se adapta.
- **Schema**: añadir `microsoft` al enum de Account.type al
  implementarlo (auto-migración lo cubre).

**Orden propuesto**: Anillo 2 = provider IMAP genérico (desbloquea
Proton/iCloud/Yahoo/Fastmail de una vez); Microsoft/Graph justo después
como Anillo 2.5 — es el único proveedor grande que el IMAP genérico no
puede cubrir.

## Descartados / lejanos

- **Exchange on-premise (EWS)**: fuera de alcance.
- **JMAP** (Fastmail): elegante pero redundante mientras IMAP funcione.
- **POP3**: no encaja con el modelo (sin estado de leído en servidor).
