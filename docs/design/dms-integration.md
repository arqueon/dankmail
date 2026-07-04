# dankmail en el ecosistema DMS — plan de publicación

Objetivo: que cualquier usuario de DMS instale la experiencia completa
en dos pasos: `paru -S dankmail` (daemon+UI, AUR) y el plugin
`dankmailUnread` desde el registry de plugins de DMS. El plugin es la
puerta de entrada; su dependencia declarada es dankmail.

## Fase 1 — Repo público + release

1. Publicar `github.com/arqueon/dankmail` (el repo local ya tiene
   LICENSE MIT + NOTICE de atribución a dankcalendar).
2. README con captura de pantalla del popup (el registry y el AUR la
   piden a gritos) y sección de instalación.
3. Tag `v0.1.0` (versión desde git describe, ya soportado por ldflags).

## Fase 2 — AUR

Dos paquetes, patrón estándar:

- **`dankmail`** (release, desde el tarball del tag) y **`dankmail-git`**.
- PKGBUILD:
  - `makedepends=(go git)`; build: `make build` (CGO_ENABLED=0, ldflags
    con versión — binario estático sin dependencias C, gracias a
    modernc/sqlite).
  - `depends=(quickshell xdg-utils)` — quickshell para la UI (el daemon
    corre sin ella); `optdepends=('gnome-keyring: almacenamiento de
    secretos', 'notification daemon: notificaciones nativas')`. El
    keyring exige un proveedor de Secret Service en sesión (DMS ya lo
    tiene cubierto en la práctica).
  - install: binario a `/usr/bin/dmail`, shell a
    `/usr/share/quickshell/dankmail` (defaultShellDir ya lo resuelve),
    icono, desktop entry, y la unidad systemd a
    `/usr/lib/systemd/user/dmail.service` (ExecStart=/usr/bin/dmail run
    --hidden — el sed del Makefile ya parametriza la ruta).
  - post_install: sugerir `systemctl --user enable --now dmail`.
- El Makefile ya tiene los targets; solo hace falta un target
  `install DESTDIR=` friendly para empaquetado (ajuste menor: respetar
  DESTDIR en las rutas).

## Fase 3 — Plugin DMS (`dms-dankmail`, id `dankmailUnread`)

Repo propio en `~/Projects/dms/dms-dankmail`, calcado de dcalUpcoming
(el plugin de barra de dankcalendar, mismo rol exacto):

- `plugin.json`: `type: widget`, `capabilities: ["dankbar-widget"]`,
  **`requires: ["dmail"]`**, `component: ./DankmailWidget.qml`,
  `settings: ./DankmailSettings.qml`.
- **v0 (publicable ya)**: badge en la dankbar con el contador de
  no-leídos — ícono `mail` + número. Datos en vivo sin polling: el
  plugin abre el socket `$XDG_RUNTIME_DIR/dankmail.sock` con
  `Quickshell.Io.Socket` (mismo protocolo línea-JSON; `subscribe` +
  `system.status` al conectar, refresco en `unread.changed`/
  `threads.changed`). Click → `Quickshell.execDetached(["dmail",
  "toggle"])` (que además resucita la UI si estaba cerrada — ya
  implementado en el daemon). Degradación: sin socket → ícono apagado
  con tooltip "dankmail no está corriendo".
- **v1**: popout al estilo dcalUpcoming con los últimos N no-leídos
  (threads.list por IPC) y acciones rápidas (leído/archivar) por
  ops.*; DND toggle en el menú del widget.
- Settings del plugin: qué cuenta mostrar (o todas), umbral de
  urgencia del badge, mostrar/ocultar en cero.

## Fase 4 — Publicación en el registry

Camino ya conocido (lo hiciste con arqueon-dmscalendar):

1. Repo del plugin en GitHub con plugin.json + screenshot + LICENSE.
2. PR a `AvengeMedia/dms-plugin-registry`: manifiesto
   `plugins/arqueon-dankmail-unread.json`, `python .github/generate.py
   --validate` + `validate_links.py` antes del PR (tu fork en
   `~/dms-plugin-registry` sigue alineado con upstream).
3. El CI de nix-prefetch corre solo; ya conoces sus mañas de hashes.

## Orden y dependencias

AUR primero (fase 1+2): el plugin declara `requires: ["dmail"]` y el
instalador de plugins de DMS muestra la dependencia — sin paquete
instalable la integración cojea. El plugin v0 es ~1 tarde de trabajo
una vez publicado el AUR.
