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
# El aprovisionamiento va en un fichero y no en un heredoc dentro de un `if`:
# mezclar las dos cosas rompio el heredoc en una version anterior y las ultimas
# lineas -- justo las que arreglan el PATH y verifican -- acabaron imprimiendose
# como texto en vez de ejecutarse. La imagen salio sin PATH y el script no se
# entero.
PROVISION_SCRIPT=$(mktemp)
trap 'rm -f "$PROVISION_SCRIPT"; kill "$RUN_PID" 2>/dev/null || true' EXIT
cat > "$PROVISION_SCRIPT" <<'PROVISION'
set -eu
export NONINTERACTIVE=1

if ! command -v brew >/dev/null 2>&1; then
  echo "    instalando homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

brew install bitomule/tap/mav bitomule/tap/axcli
curl -fsSL https://cua.ai/driver/install.sh | bash

# En una sesion SSH no interactiva el PATH es /usr/bin:/bin:/usr/sbin:/sbin, sin
# /opt/homebrew/bin. Sin esto, todo lo que acaba de instalarse es invisible al
# conducir la VM desde fuera -- incluido desde crabbox.
LINE='export PATH=/opt/homebrew/bin:/usr/local/bin:$PATH'
grep -qs "/opt/homebrew/bin" "$HOME/.zshenv" 2>/dev/null || echo "$LINE" >> "$HOME/.zshenv"
grep -qs "/opt/homebrew/bin" "$HOME/.bashrc" 2>/dev/null || echo "$LINE" >> "$HOME/.bashrc"

# Verificar, no confiar: `brew install` con varias formulas falla entero si una
# no existe, y sin esta comprobacion el script reportaba exito con la imagen a
# medio construir. Una imagen incompleta anunciada como lista es peor que un
# fallo: el error reaparece en el primer run, sin relacion aparente con la causa.
missing=""
for t in mav cua-driver axcli; do
  if command -v "$t" >/dev/null 2>&1; then
    printf "      %-10s %s\n" "$t" "$(command -v "$t")"
  else
    printf "      %-10s NO INSTALADO\n" "$t"
    missing="$missing $t"
  fi
done
[ -z "$missing" ] || { echo "faltan herramientas:$missing" >&2; exit 1; }
PROVISION

if ! SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" \
  'bash -s' < "$PROVISION_SCRIPT"
then
  echo "el aprovisionamiento fallo; la VM $NAME queda en pie para inspeccionarla" >&2
  trap 'rm -f "$PROVISION_SCRIPT"' EXIT
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
