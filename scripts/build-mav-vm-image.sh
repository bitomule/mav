#!/bin/sh
# Construye una imagen de tart con mav y sus drivers de macOS ya instalados.
#
# Lo que NO hace, a propósito: conceder permisos TCC. Accessibility y Screen
# Recording van a las herramientas y podrían hornearse, pero los que pide la app
# bajo prueba (micrófono, calendario, Apple Events...) dependen de cada app, así
# que una imagen "con permisos" sería una imagen por app. Esta trae herramientas;
# los permisos se conceden después, contra la app concreta.
#
# Transparente respecto a tu setup de tart: no toca TART_HOME ni asume dónde
# guardas las imágenes. Si lo tienes en un disco externo, se usa el tuyo.
set -eu

BASE="${MAV_VM_BASE:-ghcr.io/cirruslabs/macos-tahoe-base:latest}"
NAME="${MAV_VM_NAME:-mav-macos}"
USER_NAME="${MAV_VM_USER:-admin}"
PASSWORD="${MAV_VM_PASSWORD:-admin}"

command -v tart >/dev/null || { echo "tart no está instalado" >&2; exit 1; }

# tart >= 2.29: en 2.28.1 `tart exec` revienta sin TTY (try! sin guard sobre el
# tamaño del terminal), y crabbox lo usa para inyectar la clave SSH. Con la
# versión vieja, el provider entero no arranca desde un script.
VERSION=$(tart --version 2>/dev/null | head -1)
case "$VERSION" in
  2.2[0-8].*|2.1*|1.*) echo "tart $VERSION es demasiado antiguo; hace falta >= 2.29" >&2; exit 1 ;;
esac

echo "==> clonando $BASE -> $NAME"
tart clone "$BASE" "$NAME"

echo "==> arrancando (headless)"
tart run "$NAME" --no-graphics &
RUN_PID=$!
trap 'kill "$RUN_PID" 2>/dev/null || true' EXIT

echo "==> esperando IP"
IP=""
for _ in $(seq 1 60); do
  IP=$(tart ip "$NAME" 2>/dev/null || true)
  [ -n "$IP" ] && break
  sleep 5
done
[ -n "$IP" ] || { echo "la VM no dio IP" >&2; exit 1; }
echo "    $IP"

echo "==> esperando SSH"
for _ in $(seq 1 60); do
  SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 \
    "$USER_NAME@$IP" true 2>/dev/null && break
  sleep 5
done

echo "==> instalando mav y drivers"
# Sin `if !`, un fallo aqui se perderia si alguien canaliza la salida del script
# a otro comando: el codigo de salida de una tuberia es el del ultimo eslabon.
if ! SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" 'bash -s' <<'PROVISION'
set -eu
export NONINTERACTIVE=1

if ! command -v brew >/dev/null 2>&1; then
  echo "    instalando homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

brew install bitomule/tap/mav steipete/tap/peekaboo bitomule/tap/axcli

# En una sesion SSH no interactiva el PATH es /usr/bin:/bin:/usr/sbin:/sbin, sin
# /opt/homebrew/bin. Cualquier cosa instalada por brew seria invisible al
# conducirla desde fuera -- incluido desde crabbox -- si no se arregla aqui.
echo 'export PATH=/opt/homebrew/bin:/usr/local/bin:$PATH' >> "$HOME/.zshenv"
echo 'export PATH=/opt/homebrew/bin:/usr/local/bin:$PATH' >> "$HOME/.bashrc"

# Verificar, no confiar: `brew install` con varias formulas falla entero si una
# no existe, y sin esta comprobacion el script reportaba exito con la imagen a
# medio construir. Una imagen incompleta que se anuncia como lista es peor que
# un fallo, porque el error aparece luego en el primer run y sin relacion
# aparente con la causa.
missing=""
for t in mav peekaboo axcli; do
  if command -v "$t" >/dev/null 2>&1; then
    printf "      %-10s %s\n" "$t" "$(command -v $t)"
  else
    printf "      %-10s NO INSTALADO\n" "$t"
    missing="$missing $t"
  fi
done
[ -z "$missing" ] || { echo "faltan herramientas:$missing" >&2; exit 1; }
PROVISION
then
  echo "el aprovisionamiento fallo; la VM $NAME queda en pie para inspeccionarla" >&2
  trap - EXIT
  exit 1
fi

echo "==> apagando"
SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" \
  'sudo shutdown -h now' 2>/dev/null || true
for _ in $(seq 1 30); do tart ip "$NAME" >/dev/null 2>&1 || break; sleep 2; done

echo
echo "Imagen lista: $NAME"
echo "  usar con crabbox:  CRABBOX_TART_IMAGE=$NAME crabbox job run <job>"
echo "  publicar:          tart push $NAME <registro>/<org>/mav-macos:latest"
echo
echo "Los permisos TCC NO vienen concedidos: dependen de la app bajo prueba."
