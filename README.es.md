<p align="center"><img src="assets/icon.png" alt="Nodelight" width="160"></p>

<p align="center"><a href="README.md">English</a> · <a href="README.fr.md">Français</a> · <a href="README.nl.md">Nederlands</a> · <a href="README.de.md">Deutsch</a> · <b>Español</b></p>

# Nodelight

Un cliente Moonlight **portátil para Windows** que se conecta a tu PC Sunshine/Apollo **a través de
Tailscale**: sin instalación, sin controlador, sin derechos de administrador y sin ningún puerto que
abrir en el router.

> **Proyecto generado por IA.** Todo el código, los scripts y la documentación de este repositorio
> los escribió **Claude Sonnet 5.5** (Anthropic), mediante Claude Code, bajo la dirección del
> propietario del repositorio («vibe coding»). El código **no ha sido auditado por una persona** y
> solo se ha **probado parcialmente en una máquina real** (compila, arranca y se muestra; un stream
> completo a través de Tailscale aún no está confirmado): úsalo bajo tu propia responsabilidad (ver
> «Estado de verificación»).

Este repositorio es un **fork de [moonlight-qt](https://github.com/moonlight-stream/moonlight-qt)**: el
código del cliente es el de Moonlight, con los cambios de Nodelight añadidos como commits encima (ver
`git log`). El repositorio original está configurado como `upstream` para traer sus actualizaciones. El
resto de este archivo describe solo lo que añade Nodelight.

Nodelight no está afiliado a Tailscale Inc., al proyecto Moonlight ni a Artemis/Apollo/Sunshine.
Tailscale es una marca de Tailscale Inc.

## Cómo funciona

| Componente | Lenguaje | Función |
| --- | --- | --- |
| `bridge/` → `Nodelight.exe` | Go | Nodo Tailscale en espacio de usuario (`tsnet`) + relé TCP/UDP local, inicia Moonlight |
| `app/`, `moonlight-common-c/`… → `client\NodelightClient.exe` | C++/Qt | El cliente Moonlight modificado: modo «remoto» para el relé, opción de pantalla virtual de Apollo tomada de Artemis, página de Tailscale integrada, nombre e icono de Nodelight |

Moonlight transmite por UDP (vídeo, audio, control) con sockets en bruto que no pueden pasar por un
proxy SOCKS5, y lee los números de puerto de las respuestas RTSP del anfitrión. Por eso Nodelight
escucha en **los mismos puertos que Sunshine** en una dirección loopback dedicada por PC
(`127.77.0.1`, `127.77.0.2`, …) y los reenvía por la tailnet.

```
Moonlight ──► 127.77.0.1:47989/47984/48010 (TCP), :47998-48002 (UDP)
                 │  Nodelight (relé + tsnet/WireGuard, espacio de usuario)
                 ▼
          PC anfitrión Sunshine/Apollo  (a través de tu tailnet)
```

Puertos reenviados por anfitrión (base 47989): TCP 47984, 47989, 48010 (+ 47990 con `--web-ui`),
UDP 47998, 47999, 48000, 48002.

## Uso

1. Haz doble clic en `Nodelight.exe`: **no se abre ninguna consola**, Moonlight arranca directamente
   (en modo portátil, gracias a un archivo `portable.dat`: los ajustes y emparejamientos se quedan en
   la carpeta). Nodelight se cierra con Moonlight. Lo que habría mostrado se escribe en
   `Nodelight.log`, y los errores de arranque aparecen en un cuadro de diálogo.
2. En el primer arranque, Moonlight abre su página **Tailscale** (un botón con un icono de red en la
   barra superior). Pulsa **Connect**: se abre una página web para autorizar el dispositivo (una sola
   vez). La identidad se guarda en `data\` (mantén esta carpeta privada; se puede copiar de un PC a
   otro). No hace falta ningún PC para conectarse.
3. Una vez conectado, elige si hace falta el **nodo de salida** (ver más abajo) y luego **Add a PC**:
   un nombre y el nombre Tailscale o la IP del PC, y después **Save PCs**. El PC se aplica al
   instante.
4. Los PC aparecen **automáticamente** en Moonlight (`127.77.0.1`, `127.77.0.2`… en el orden de la
   lista; no los reordenes si ya hay PC emparejados). Empareja como de costumbre.

Si se inicia desde un terminal, `Nodelight.exe` escribe en ese terminal y hace las preguntas del
primer arranque (útil con `--json` o `--no-launch`).

Opciones útiles: `--host nombre=dirección[:puertoBase]` (repetible), `--hostname`, `--state-dir`,
`--no-browser`, `--json` (eventos línea a línea), `--listen-prefix`, `--web-ui`, `--control-url` para
un servidor Headscale autoalojado, y `--exit-node` (ver más abajo).

### Página de configuración

La conexión, el nodo de salida, los PC y los perfiles se ajustan sin editar archivos, de dos maneras:

- **En Moonlight**: botón **Tailscale (Nodelight)** (icono de red) en la barra superior. La página
  tiene controles en los que se puede hacer clic para: el estado de la conexión y **Connect /
  Disconnect** (más un botón para abrir la página de inicio de sesión de Tailscale), el **nodo de
  salida** (lista de tu tailnet + **Apply exit node**), la **lista de PC** (nombre, dirección, puerto
  base, dirección local `127.77.0.N`, punto verde cuando el PC responde, **Add a PC** / ✕ / **Save
  PCs**) y los **perfiles** (cambiar, eliminar, crear). El botón solo aparece si Moonlight fue
  iniciado por Nodelight. Una sección equivalente, más corta, permanece en Ajustes.
- **En el navegador** (alternativa): la dirección se escribe en `Nodelight.log`
  (`Settings page: http://127.0.0.1:PUERTO/?token=…`) o en el terminal; mismo contenido.

Un **perfil** agrupa una identidad Tailscale, un nombre de dispositivo, un servidor de control
opcional (Headscale) y una lista de PC. Permite pasar de una red Tailscale a otra; cada perfil guarda
su identidad en `data\profiles\<nombre>`, el perfil por defecto se queda en `data\`. Los perfiles se
guardan en `hosts.json` (los archivos de versiones anteriores siguen siendo válidos).

Notas:
- Desconectar, cambiar de perfil o de nodo de salida **interrumpe el stream en curso**.
- Un valor dado en la línea de comandos (`--exit-node`, `--host`…) se aplica al arrancar sin
  guardarse; una elección hecha después en la página lo reemplaza.
- Seguridad: el servidor solo escucha en `127.0.0.1`, cada llamada exige un token aleatorio generado
  en cada arranque (pasado a Moonlight por variable de entorno), y se rechazan las peticiones cuyo
  encabezado `Host` no sea la dirección local (protección contra el «DNS rebinding»). Otro programa
  que se ejecute en tu sesión de Windows sí puede leer ese entorno: no es una barrera de seguridad
  contra código que ya se ejecuta en tu PC.

### PC anfitrión que no está en Tailscale (nodo de salida)

Si tu PC Sunshine/Apollo no tiene Tailscale pero otra máquina de su red local (Home Assistant, NAS,
Raspberry Pi…) está en tu tailnet **como nodo de salida**, Nodelight puede pasar por ella:

```json
{
  "exitNode": "homeassistant",
  "hosts": [ { "name": "escritorio", "address": "192.168.1.20" } ]
}
```

- `exitNode`: nombre o IP Tailscale del nodo de salida (o `--exit-node`, o la lista de la página
  Tailscale). `address` es entonces la **dirección local** del PC anfitrión (`192.168.x.y`), vista
  desde la red del nodo de salida.
- El nodo de salida debe estar aprobado en la consola de administración de Tailscale (Machines →
  Edit route settings → Use as exit node). El dispositivo `nodelight` debe tener permiso para usarlo
  (así es con las ACL por defecto).
- Mismo principio que una máquina remota que usa ese nodo de salida: si tu PC alcanza así la dirección
  local del anfitrión, Nodelight debería poder hacerlo también. **Sin probar**: no pude comprobar que
  la biblioteca `tsnet` enrute realmente el tráfico UDP del streaming a una dirección local por un
  nodo de salida. Es lo primero que hay que probar.
- Rendimiento: todo el flujo de vídeo pasa por la máquina del nodo de salida, cifrado con WireGuard.
  Un pequeño servidor domótico puede limitar el caudal; baja la tasa de bits del vídeo en Moonlight si
  la imagen se entrecorta. Si el anfitrión puede tener Tailscale, siempre es más sencillo y rápido
  (conexión directa, dirección `100.x` o nombre MagicDNS en lugar de la IP local, y sin nodo de salida).

### Lado del anfitrión (Sunshine/Apollo)

- El PC anfitrión debe estar en la misma tailnet y encendido, con Sunshine/Apollo en marcha. Nodelight
  indica al arrancar si cada anfitrión responde en su puerto HTTP.
- El cortafuegos de Windows del anfitrión debe permitir Sunshine/Apollo en la interfaz de Tailscale
  (perfil de red correspondiente); tus ACL de Tailscale deben permitir estos puertos.
- Por defecto, Sunshine solo permite el emparejamiento (`origin_pin_allowed`) desde la red local: si se
  rechaza el emparejamiento, ajusta esa opción o empareja una primera vez en la LAN.
- Para un buen rendimiento hace falta una conexión Tailscale **directa** (no a través de un relé DERP);
  el cifrado WireGuard en espacio de usuario consume algo de CPU.

## Obtener el build sin entorno de desarrollo

El binario se construye en las máquinas Windows gratuitas de GitHub Actions:

1. Pestaña **Actions** → **Build Windows x64 (portable)** → **Run workflow** (de 30 a 60 minutos; un
   `push` a `master` también lo inicia). Los workflows originales de Moonlight (AppImage, Steam Link,
   firma de Windows/macOS) se han eliminado de este fork: necesitan secretos que aquí no existen.
2. Descarga el artefacto `Nodelight-win-x64` (un zip) y descomprímelo: eso es todo.

El zip contiene el runtime de Visual C++ (copia local): sin requisitos en la máquina de destino. No se
pide elevación de privilegios (manifiesto `asInvoker`, puente en espacio de usuario, puertos locales
> 1024 solo en loopback). El ejecutable no está firmado: SmartScreen puede pedir confirmación, y el
cortafuegos de Windows puede proponer un permiso de red que puedes cancelar.

## Compilación manual (Windows x64)

Requisitos: Go 1.26+, Git, Qt 6.11+ (MSVC), Visual Studio 2026 (o la versión que exija moonlight-qt),
PowerShell.

```powershell
git clone --recurse-submodules https://github.com/BKNadir/Nodelight
cd Nodelight
.\scripts\build-bridge.ps1      # pruebas + dist\Nodelight.exe
.\setup-deps.ps1
scripts\build-arch.bat release  # desde un símbolo del sistema de Qt + MSVC
.\scripts\package.ps1           # dist\Nodelight-win-x64.zip
```

## Qué se toma de Artemis y qué no

Artemis es un cliente **Android** para Apollo. Muchas de sus funciones no tienen sentido en Windows o
ya existen en moonlight-qt.

| Función de Artemis | Estado aquí |
| --- | --- |
| Pantalla virtual de Apollo | **Hecho**: casilla en los ajustes del anfitrión + `--virtual-display` |
| Resolución y tasa de bits personalizadas | Ya presentes en moonlight-qt |
| Comandos de servidor de Apollo | No hecho: requiere el fork de `moonlight-common-c` de Artemis (`LiSendExecServerCmd`) |
| Sincronización del portapapeles | No hecho: llamadas HTTP `actions/clipboard` + integración con `QClipboard` por escribir |
| Botones virtuales, trackpad táctil, DeX, vertical, SBS 3D… | Específicas de Android, no aplicables |

## Estado de verificación — léelo

Verificado en el entorno de desarrollo (sin Windows):
- pruebas del relé (TCP de ida y vuelta con cierre a medias, flujos UDP independientes por cliente,
  caducidad de flujos inactivos, enlace «todo o nada», cierre limpio): OK con `-race`;
- pruebas del análisis de la configuración, de los perfiles y de la entrada del primer arranque;
- pruebas del controlador (conexión con o sin PC, desconexión, conexión a la espera de autorización,
  errores, cambio del nodo de salida en caliente, lista de PC, cambio y eliminación de perfil) y de la
  API local (token, protección `Host`, validación), con un Tailscale falso, ejecutadas varias veces
  con `-race`;
- la lógica JavaScript de las páginas QML se ejecutó bajo Node con los JSON reales del código Go;
- los cambios del cliente se aplican limpiamente sobre el commit `3178022` de moonlight-qt.

Comprobado por el autor en Windows: el build de GitHub Actions termina, la aplicación arranca, los
iconos aparecen en los lugares correctos y la página de Tailscale se abre dentro de Moonlight.

**No verificado** (por probar):
- la conexión real a Tailscale desde la aplicación (autorización, lista de nodos de salida);
- el enrutamiento real del tráfico por un nodo de salida con `tsnet` (UDP en particular);
- un stream completo a través de Tailscale, y el comportamiento del cortafuegos/ACL;
- el acoplamiento al terminal, el archivo de registro y los cuadros de diálogo de `Nodelight.exe` en
  todos los casos.

Si falla algún paso, abre un issue con el mensaje de error (y `Nodelight.log`): las causas más
probables son un desajuste de la API de `tsnet` (a corregir en `bridge/cmd/nodelight/tunnel_ts.go`) o
un puerto ya en uso en la dirección loopback (un Sunshine local, por ejemplo).

## Firma de código

Los ejecutables **aún no están firmados** (SmartScreen puede avisar: «Más información» → «Ejecutar de todas formas»). Hay una solicitud en curso para la firma gratuita de la [SignPath Foundation](https://signpath.org/). Política completa (en inglés): [Code signing policy](README.md#code-signing-policy).

## Licencia

GPL-3.0 (ver `LICENSE`): el código deriva de moonlight-qt. Atribuciones detalladas en
`licenses/NOTICE.md`.
