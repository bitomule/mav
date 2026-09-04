package macos

import (
	"context"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// On the Mac the binary is the app, so there is no prefix to translate: the
// variables go straight onto the process being started.
func TestLaunchCarriesTheEnvironmentOntoTheBinary(t *testing.T) {
	app := makeBundle(t, "Nokoru", "Nokoru")
	f := &fakeExec{}
	spec := drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"FOO": "bar"}}
	if _, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, spec); err != nil {
		t.Fatal(err)
	}
	command := f.commands[0]
	if !strings.HasPrefix(command, "/usr/bin/env FOO=bar ") {
		t.Fatalf("the variable must be set for the app: %q", command)
	}
	if !strings.Contains(command, "Contents/MacOS/Nokoru") {
		t.Fatalf("the bundle's binary must still be what runs: %q", command)
	}
}
