# Correr `mav` contra una app de macOS dentro de una VM desechable

Esta es la Fase 4 del [análisis de scope](../../docs/macos-scope-evaluation.md), y lo
importante es lo que **no** hay aquí: código de MAV. MAV no orquesta máquinas. Envuelve
drivers y produce evidencia; la máquina la pone [crabbox](https://github.com/openclaw/crabbox),
que ya sabe alquilar una VM de macOS con `tart`, sincronizar el checkout sucio, ejecutar y
devolverla al acabar.

## Por qué molestarse

En tu propio Mac, validar una app de macOS tiene dos problemas que no se arreglan con
código:

1. **TCC.** Una app real pide varios permisos (micrófono, calendario, Apple Events…). O ya
   se los concediste —y entonces tu test no parte de limpio— o te comes un prompt a mitad
   del run. Y Screen Recording y micrófono son *deny-only* en PPPC: ni un admin de MDM
   puede pre-autorizarlos.
2. **Estado.** `--clear-state` borra el contenedor de la app, pero no el resto de rastro
   que deja en el sistema.

En una VM cuya imagen fabricas tú, los dos desaparecen: las imágenes base de tart traen
**SIP desactivado**, así que el aprovisionamiento escribe los permisos directamente en
`TCC.db`, y el estado se tira al borrar la VM.

## La receta

`.crabbox.yaml` en la raíz del repo:

```yaml
provider: tart
tart:
  image: ghcr.io/cirruslabs/macos-tahoe-base:latest
  user: admin
  cpus: 4
  memory: 8192
```

Y en `.mav/config.yaml`, el perfil declara dónde corre:

```yaml
profiles:
  mac-vm:
    target_kind: macos
    runner: crabbox
    app_target: "//App:MyAppMac"
```

Aprovisionamiento (una vez por caja, en el warmup de crabbox):

```sh
#!/bin/sh
# scripts/provision-mav-vm.sh
set -eu

brew install bitomule/tap/mav steipete/tap/peekaboo bitomule/tap/axcli

# Los permisos que en tu Mac son un click humano aquí son una fila de SQLite,
# porque la imagen base trae SIP desactivado. Este es el motivo entero de usar
# una VM en vez del Mac de al lado.
TCC="/Library/Application Support/com.apple.TCC/TCC.db"
for service in kTCCServiceAccessibility kTCCServiceScreenCapture; do
  sudo sqlite3 "$TCC" "INSERT OR REPLACE INTO access
    (service, client, client_type, auth_value, auth_reason, auth_version)
    VALUES ('$service', '/bin/sh', 1, 2, 4, 1);"
done
```

Y el run:

```sh
crabbox run --provider tart -- mav run flows/smoke.yaml --profile mac-vm
```

## Lo que no funciona todavía

`crabbox run --artifact-glob` **rechaza los targets nativos de macOS**, que es justo el
mecanismo con el que sacarías `.mav/runs/<id>/` de la VM. Está identificado aguas arriba en
[crabbox#1393](https://github.com/openclaw/crabbox/issues/1393). Mientras tanto:

```sh
crabbox warmup --provider tart          # imprime el slug del lease
crabbox run --id <slug> -- mav run flows/smoke.yaml --profile mac-vm
rsync -a "$(crabbox ssh --id <slug> --print-target)":.mav/runs/ ./.mav/runs/
```

La salida de `mav` en sí no necesita nada de esto: su línea `ok cmd=… k=v` vuelve por
stdout tal cual. Lo que se queda dentro es la evidencia visual.

## Dos límites que conviene saber antes de montarlo

- **Máximo 2 VMs de macOS concurrentes.** Es límite de `Virtualization.framework` *y* del
  EULA de macOS, no se salta con más RAM. Por eso el leasing de crabbox pasa de cómodo a
  obligatorio.
- **El provider `tart` de crabbox no expone `--audio`.** Si lo que validas necesita
  micrófono, esa VM no lo tendrá aunque tart sí sepa hacerlo.
