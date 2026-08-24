package mav

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCompactOutputSortsAndQuotes(t *testing.T) {
	var b bytes.Buffer
	err := OK("capture", map[string]string{
		"file": "/tmp/mav/abc/screen.png",
		"next": "review screenshot",
	}).Write(&b)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(b.String())
	want := `ok cmd=capture file=/tmp/mav/abc/screen.png next="review screenshot"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFailOutput(t *testing.T) {
	var b bytes.Buffer
	// Un fallo devuelve CommandFailed ADEMAS de escribir la linea: es lo que
	// hace que el codigo de salida del proceso coincida con lo que dice la
	// salida. Escribir y devolver nil dejaba `mav ... && siguiente` encadenando
	// despues de un fallo.
	var failed CommandFailed
	if err := Fail("screen_not_found", map[string]string{"screen": "settings"}).Write(&b); !errors.As(err, &failed) {
		t.Fatalf("un fallo debe propagarse: %v", err)
	}
	got := strings.TrimSpace(b.String())
	want := "fail code=screen_not_found screen=settings"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// allowFail acepta el error que ahora acompana a una linea `fail`, y sigue
// tratando cualquier otro como error de verdad. Los tests que lo usan ya
// afirman el codigo concreto en stdout, que es la comprobacion util; lo que no
// pueden es exigir exito.
func allowFail(t *testing.T, err error) {
	t.Helper()
	var failed CommandFailed
	if err != nil && !errors.As(err, &failed) {
		t.Fatal(err)
	}
}
