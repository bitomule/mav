package mav

import (
	"context"
	"strings"
	"time"
)

// El control de tiempo en macOS es de SISTEMA, no por proceso, y esa diferencia
// con iOS no es un detalle de implementacion sino el hecho central.
//
// En simulador, simtime interpone el reloj que ve la app y no toca la maquina.
// En macOS no existe ese equivalente: la unica via por proceso es libfaketime
// con DYLD_INSERT_LIBRARIES, que el hardened runtime bloquea en cualquier app
// firmada para distribuir -- o sea, funciona sobre tu build de Debug y sobre
// nada mas. Lo que si funciona siempre es cambiar el reloj de la maquina, y por
// eso es lo que mav ofrece, con la puerta cerrada por defecto.
//
// `freeze` y `scale` no se mapean a nada: un reloj de sistema corre, y no hay
// forma de pararlo ni de acelerarlo.

// macClockInVM dice si esto es un invitado de Virtualization.framework.
//
// Es la puerta: cambiar el reloj de una VM dedicada es barato y reversible;
// hacerlo en el Mac de alguien le caduca certificados, le rompe sesiones y le
// desordena los ficheros por fecha. Con --system-clock se puede forzar, porque
// hay quien sabe lo que hace, pero tiene que decirlo.
func (c CLI) macClockInVM(ctx context.Context) bool {
	res := c.Runner.Run(ctx, "sysctl", "-n", "kern.hv_vmm_present")
	return strings.TrimSpace(res.Stdout) == "1"
}

// macTimeTravel lleva el reloj de la maquina a un instante.
//
// La sincronizacion de red se apaga primero porque, si no, el sistema deshace
// el cambio en cuanto le apetece consultar la hora -- y el sintoma seria una
// prueba que pasa o falla segun el momento en que se ejecute.
func (c CLI) macTimeTravel(ctx context.Context, at time.Time) error {
	if res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setusingnetworktime", "off"); res.Err != nil {
		return res.Err
	}
	local := at.Local()
	if res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setdate", local.Format("01:02:06")); res.Err != nil {
		return res.Err
	}
	res := c.Runner.Run(ctx, "sudo", "systemsetup", "-settime", local.Format("15:04:05"))
	return res.Err
}

// macTimeReset devuelve el reloj al del mundo real.
func (c CLI) macTimeReset(ctx context.Context) error {
	res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setusingnetworktime", "on")
	return res.Err
}

// macTimeStatus reporta lo que hay, que en un reloj de sistema es la hora y si
// alguien la esta sincronizando por debajo.
func (c CLI) macTimeStatus(ctx context.Context) map[string]string {
	fields := map[string]string{"clock": "system"}
	if res := c.Runner.Run(ctx, "date", "-u", "+%Y-%m-%dT%H:%M:%SZ"); strings.TrimSpace(res.Stdout) != "" {
		fields["now"] = strings.TrimSpace(res.Stdout)
	}
	res := c.Runner.Run(ctx, "systemsetup", "-getusingnetworktime")
	if strings.Contains(strings.ToLower(res.Stdout), "on") {
		fields["network_time"] = "on"
	} else if strings.Contains(strings.ToLower(res.Stdout), "off") {
		fields["network_time"] = "off"
	}
	return fields
}
