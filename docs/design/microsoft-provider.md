# Provider `microsoft` — Outlook.com / Microsoft 365 vía Graph API

Diseño del provider nativo de Microsoft (Anillo 2.5 del roadmap). Espejo
del provider Gmail: mismo contrato `provider.Provider`, mismo patrón de
seam para tests, mismo wizard servido por IPC. Este documento fija las
decisiones; el código las implementa tal cual.

Estado: **fundaciones en main** (broker OAuth parametrizado por
endpoints con PKCE, `GraphScopes`, enum `microsoft` en Account.type);
el provider en sí está por implementarse.

## 1. Autenticación

- **App registration de Azure** creada por el usuario (mismo modelo
  bring-your-own-client que Gmail): public client (Mobile & desktop),
  redirect `http://localhost` (loopback), "Allow public client flows"
  activado, supported account types = *Personal Microsoft accounts and
  work/school accounts* (tenant `common`).
- **Sin client secret**: el broker ya hace PKCE S256 en todos los flujos;
  `oauth.NewBrokerFor(oauth.MicrosoftEndpoints, clientID, "", bindAddr)`.
- **Scopes** (`oauth.GraphScopes`): `Mail.ReadWrite` + `Mail.Send` +
  `User.Read` + `offline_access`. Regla de minimalidad de Gmail
  (spec §3.2) aplicada a Graph: jamás `Mail.ReadWrite.Shared` ni scopes
  de directorio.
- El email de la cuenta se lee del perfil autorizado
  (`GET /me` → `mail` o `userPrincipalName`), como `FetchGmailEmail`.
- Token + client ID al llavero con las claves existentes
  (`KeyOAuthToken`/`KeyOAuthClient`; ClientCreds.ClientSecret queda "").

## 2. Seam `graphAPI` (tests sin HTTP)

Interfaz mínima sobre el cliente REST de Graph — todo lo que el provider
toca y nada más (patrón `gmailAPI` de anillo1 §2):

```go
type graphAPI interface {
    GetProfile(ctx) (email string, err error)                    // GET /me
    DeltaMessages(ctx, folder, deltaLink string) (deltaPage, error) // GET /me/mailFolders/{folder}/messages/delta
    GetMessage(ctx, id string) (*graphMessage, error)            // GET /me/messages/{id} (cuerpo + headers)
    ListConversation(ctx, convID string) ([]*graphMessage, error) // GET /me/messages?$filter=conversationId eq '…'
    PatchMessage(ctx, id string, body map[string]any) error      // PATCH /me/messages/{id} (isRead, flag)
    MoveMessage(ctx, id, destFolder string) (newID string, err error) // POST /me/messages/{id}/move
    SendMail(ctx, mime []byte) error                             // POST /me/sendMail (MIME base64)
    FolderIDs(ctx) (map[string]string, error)                    // GET /me/mailFolders (well-known names→ids)
}
```

Sin SDK oficial: cliente HTTP fino sobre `oauth2.NewClient` (el SDK de
Graph para Go es enorme y genera fricción de versiones; los endpoints
usados son seis). Implementación real en `graph_client.go`, fake en
`graph_fake.go` (tests).

## 3. Modelo de hilos: message-centric → thread-centric

**El problema central** (Gmail no lo tiene): Graph es de mensajes; el
hilo de dankmail es `provider_thread_id` + delta agregado.

- `provider_thread_id` = `conversationId` de Graph (estable por buzón).
- El **delta de mensajes** llega por `/messages/delta` sobre las
  carpetas monitoreadas (`inbox` + `junkemail`; ver §5). Cada página
  trae mensajes creados/cambiados/borrados.
- Para cada `conversationId` afectado en el lote, el provider llama
  `ListConversation` y construye el `ThreadDelta` completo (subject,
  participantes, unread = algún mensaje `!isRead`, starred = algún
  `flag.flagStatus == "flagged"`, InInbox = algún mensaje con
  `parentFolderId == inbox`, labels = carpetas well-known presentes
  {SPAM si junkemail, TRASH si deleteditems}) — mismo shape que
  `threadDelta()` de gmail.go, así el reconciler no cambia NADA.
- Mensajes: cuerpo `body.content` (text) o distiller HTML→texto ya
  existente si `contentType == "html"`; adjuntos =
  `hasAttachments` + `GET /messages/{id}/attachments?$select=name,contentType,size`
  (metadata only, spec §1).

## 4. Sync

- **Cursor** = JSON `{ "inbox": "<deltaLink>", "junkemail": "<deltaLink>" }`
  serializado en `account.sync_cursor` (un deltaLink por carpeta
  monitoreada) ⇒ `CapHistorySync`.
- **Full sync**: delta inicial sin deltaLink (Graph pagina todo el
  folder y entrega el deltaLink final). Mismo guard de replay que
  Gmail.
- **Incremental**: delta con deltaLink; si Graph responde
  `SyncStateNotFound` (410) → full resync, como el cursor expirado de
  Gmail.
- `dmail sync --full` ya limpia el cursor (funciona sin cambios).

## 5. Triage (mapeo de ops)

| Op dankmail | Graph | Nota |
|---|---|---|
| read/unread | `PATCH isRead` por mensaje | El executor coalesce por hilo; el provider expande a los mensajes del hilo (como batchModify de Gmail). |
| star/unstar | `PATCH flag.flagStatus = flagged/notFlagged` | |
| archive | `POST /move` a `archive` | Well-known folder. |
| unarchive | `POST /move` a `inbox` | |
| trash | `POST /move` a `deleteditems` | |
| snooze | local (igual que Gmail) | `CapServerSnooze` sigue reservado. |
| reply/send | `POST /sendMail` con MIME de `mailmime` | Threading por In-Reply-To/References ya resuelto en mailmime. |

⇒ `CapModifyFlags | CapArchive | CapTrash | CapSendReply | CapCompose |
CapHistorySync | CapDeepLink`.

- **Spam**: la carpeta `junkemail` se monitorea siempre (paridad con la
  vista Spam de Gmail); sus hilos llevan label `SPAM`, `InInbox=false`.
- **Deep links**: `webLink` del mensaje más reciente ⇒ mejor que Gmail
  (no hay que construir la URL).
- **Contactos**: fuera del MVP (People API de Graph = scope extra);
  el autocomplete cae a correspondientes locales.

## 6. Wizard y wiring

- `accounts.microsoft.setupGuide/start/complete` — mismos tres IPC que
  Gmail; `complete` comparte el `flowRegistry`. El texto de los pasos de
  Azure se adapta del de dankcalendar (incluida la trampa del
  directorio/suscripción en cuentas personales).
- `accounts.reauth` ya existente: gana un switch por tipo de cuenta para
  elegir `MicrosoftEndpoints` (hoy asume Gmail).
- `registry.go`: `case account.TypeMicrosoft:` → broker Microsoft +
  `microsoft.New(...)` con el token source persistente.
- QML: tercer proveedor en el chooser del wizard ("Outlook / Microsoft
  365"), formulario solo pide Client ID (no hay secret ni JSON).
- CLI: `dmail account add-microsoft [--client-id …]`.

## 7. Orden de implementación (testeable primero, como anillo1)

1. `graph_client.go` + `graph_fake.go` (seam + fake con fixtures).
2. `microsoft.go`: threadDelta/conversation grouping + fullSync — tests
   con fake.
3. Incremental delta + expiración de cursor — tests.
4. Ops de triage + Send — tests.
5. Wiring: registry, wizard IPC, CLI, QML, reauth por tipo.
6. Docs: README (sección Accounts), microsoft-setup.md.

## 8. Qué necesita el usuario (una vez)

1. <https://portal.azure.com> → **App registrations** → *New
   registration*: nombre libre (p. ej. "dankmail"), supported account
   types = **Personal Microsoft accounts and work/school accounts**.
2. **Authentication** → *Add a platform* → **Mobile and desktop
   applications** → redirect URI `http://localhost` → y activar **Allow
   public client flows = Yes**.
3. Copiar el **Application (client) ID** — es lo único que pide el
   wizard (no se crea ningún secret).

Cuentas personales (hotmail.com/outlook.com/live.com) funcionan con
tenant `common` sin suscripción de Azure de pago.
