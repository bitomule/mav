package axe

import (
	"errors"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestAXeReceiptSelectorFailureCannotClearUncertainCleanup(t *testing.T) {
	for _, receipt := range []string{
		`{"outcome":"failedBeforeInput","enqueuedEventCount":-1,"cleanup":"released"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"futureCleanup"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"complete"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0}`,
		`{"outcome":"unknownFuture","enqueuedEventCount":0,"cleanup":"released"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"uncertain"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"released","quarantine":{"reason":"sessionUncertain"}}`,
	} {
		raw := `{"command":"tap","schema":"axe.output.command-result","schemaVersion":"1","didError":true,"error":{"code":"ambiguousElement","message":"ambiguous"},"data":{"result":` + receipt + `}}`
		_, err := decodeOutput(drivers.ExecResult{Stdout: raw, Code: 1}, true)
		var native *drivers.DriverError
		if !errors.As(err, &native) || native.Delivery != drivers.DeliveryUncertain {
			t.Fatalf("selector error erased uncertain native receipt: %v", err)
		}
	}
}

func TestAXeReceiptPreservesEarlierUnknownCleanup(t *testing.T) {
	raw := `{"command":"batch","schema":"axe.output.command-result","schemaVersion":"1","didError":true,"error":{"code":"inputFailed","message":"failed"},"data":{"results":[{"outcome":"unknownFuture","enqueuedEventCount":0,"cleanup":"futureCleanup"},{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"notNeeded"}]}}`
	_, err := decodeOutput(drivers.ExecResult{Stdout: raw, Code: 1}, true)
	var native *drivers.DriverError
	if !errors.As(err, &native) || native.Delivery != drivers.DeliveryUncertain {
		t.Fatalf("later clean receipt erased earlier unknown cleanup: %v", err)
	}
}

func TestAXeReceiptAggregationCannotEraseEarlierUncertainDelivery(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"unknown outcome", `{"outcome":"unknownFuture","enqueuedEventCount":0,"cleanup":"released"}`, `{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"notNeeded"}`},
		{"invalid negative count cancels delivered events", `{"outcome":"enqueuedAndDrained","enqueuedEventCount":2,"cleanup":"released"}`, `{"outcome":"failedBeforeInput","enqueuedEventCount":-2,"cleanup":"notNeeded"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"command":"batch","schema":"axe.output.command-result","schemaVersion":"1","didError":true,"error":{"code":"inputFailed","message":"failed"},"data":{"results":[` + tc.first + `,` + tc.second + `]}}`
			_, err := decodeOutput(drivers.ExecResult{Stdout: raw, Code: 1}, true)
			var native *drivers.DriverError
			if !errors.As(err, &native) || native.Delivery == drivers.DeliveryNotSent {
				t.Fatalf("aggregation made uncertain/previously delivered input replayable: %+v", native)
			}
		})
	}
}

func TestAXeReceiptAggregateSuccessCannotHideEarlierFailure(t *testing.T) {
	raw := `{"command":"batch","schema":"axe.output.command-result","schemaVersion":"1","didError":false,"data":{"results":[{"outcome":"failedNoReplay","enqueuedEventCount":2,"cleanup":"released"},{"outcome":"enqueuedAndDrained","enqueuedEventCount":2,"cleanup":"released"}]}}`
	_, err := decodeOutput(drivers.ExecResult{Stdout: raw}, true)
	var native *drivers.DriverError
	if !errors.As(err, &native) || native.Delivery == drivers.DeliveryNotSent {
		t.Fatalf("later completed receipt hid earlier partial failure: %v", err)
	}
}
