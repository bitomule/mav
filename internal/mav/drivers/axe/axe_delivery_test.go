package axe

import (
	"errors"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func failedEnvelope(command, code string) drivers.ExecResult {
	return drivers.ExecResult{Code: 1, Err: errors.New("exit status 1"), Stdout: `{"command":"` + command + `","data":{},"didError":true,"error":{"code":"` + code + `","message":"m"},"nextSteps":[],"schema":"axe.output.command-result","schemaVersion":"1"}`}
}

// AXe resolves a tap or slider selector before it sends any HID input, so a
// selector failure there is safe to retry.
func TestSelectorFailuresBeforeInputAreNotSent(t *testing.T) {
	for _, tc := range []struct {
		command, code string
		want          drivers.InputDelivery
	}{
		{"tap", "elementNotFound", drivers.DeliveryNotSent},
		{"tap", "ambiguousElement", drivers.DeliveryNotSent},
		{"tap", "elementNotHittable", drivers.DeliveryNotSent},
		{"slider", "elementNotFound", drivers.DeliveryNotSent},
		{"tap", "rejectedBeforeInput", drivers.DeliveryNotSent},
		// A scroll-until can fail elementNotFound after it has scrolled.
		{"gesture", "elementNotFound", drivers.DeliveryUncertain},
		{"swipe", "elementNotFound", drivers.DeliveryUncertain},
		{"batch", "elementNotFound", drivers.DeliveryUncertain},
		{"batch", "rejectedBeforeInput", drivers.DeliveryUncertain},
		{"tap", "inputFailed", drivers.DeliveryUncertain},
	} {
		_, err := decodeOutput(failedEnvelope(tc.command, tc.code), true)
		var typed *drivers.DriverError
		if !errors.As(err, &typed) {
			t.Fatalf("%s %s: error=%v", tc.command, tc.code, err)
		}
		if typed.Delivery != tc.want {
			t.Errorf("%s %s: delivery=%s, want %s", tc.command, tc.code, typed.Delivery, tc.want)
		}
	}
}
