package axe

import (
	"encoding/json"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type commandEnvelope struct {
	Command       string `json:"command"`
	Schema        string `json:"schema"`
	SchemaVersion string `json:"schemaVersion"`
	DidError      *bool  `json:"didError"`
	Data          struct {
		Result   json.RawMessage   `json:"result"`
		Results  []json.RawMessage `json:"results"`
		Messages []string          `json:"messages"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeOutput(res drivers.ExecResult, mutation bool) (drivers.NativeResult, error) {
	var envelope commandEnvelope
	raw := json.RawMessage(strings.TrimSpace(res.Stdout))
	delivery := drivers.DeliveryNotSent
	if mutation {
		delivery = drivers.DeliveryUncertain
	}
	fail := func(code, message string, receipt *drivers.InputReceipt) (drivers.NativeResult, error) {
		return drivers.NativeResult{Envelope: raw, Receipt: receipt}, &drivers.DriverError{Driver: ID, Code: code, Message: message, Delivery: delivery, Receipt: receipt, Details: raw}
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Schema != "axe.output.command-result" || envelope.SchemaVersion != "1" || envelope.DidError == nil {
		return fail("axe_invalid_response", "AXe did not return a valid command-result JSON response; "+failureText(res), nil)
	}
	result := drivers.NativeResult{Data: envelope.Data.Result, Envelope: raw}
	if len(envelope.Data.Results) > 0 {
		result.Data, _ = json.Marshal(envelope.Data.Results)
	}
	values := append([]json.RawMessage{}, envelope.Data.Results...)
	if len(envelope.Data.Result) > 0 {
		values = append(values, envelope.Data.Result)
	}
	receiptDelivery := drivers.DeliveryNotSent
	receiptsComplete := true
	for _, v := range values {
		if receipt := findReceipt(v); receipt != nil {
			// Classify each original receipt before aggregation can replace its
			// outcome or let a malformed negative count cancel earlier events.
			state := nativeReceiptDelivery(receipt)
			if state == drivers.DeliveryUncertain || receiptDelivery == drivers.DeliveryUncertain {
				receiptDelivery = drivers.DeliveryUncertain
			} else if state == drivers.DeliverySent {
				receiptDelivery = drivers.DeliverySent
			}
			if receiptRecoveryUncertain(receipt) || receipt.EnqueuedEventCount < 0 || receipt.Outcome != "enqueuedAndDrained" {
				receiptsComplete = false
			}
			if result.Receipt == nil {
				result.Receipt = receipt
			} else {
				prior := result.Receipt
				receipt.EnqueuedEventCount += prior.EnqueuedEventCount
				if receiptRecoveryUncertain(prior) {
					receipt.Cleanup = prior.Cleanup
				}
				if nonNull(prior.Quarantine) {
					receipt.Quarantine = prior.Quarantine
				}
				result.Receipt = receipt
			}
		}
	}
	if result.Receipt != nil {
		delivery = receiptDelivery
	}
	if *envelope.DidError || res.Err != nil || res.Code != 0 {
		code, message := "axe_command_failed", failureText(res)
		if envelope.Error != nil {
			code, message = envelope.Error.Code, envelope.Error.Message
			// A native receipt is stronger evidence than a command-level code.
			// Never erase partial delivery or uncertain recovery with this inference.
			if result.Receipt == nil && failedBeforeInput(envelope.Command, code) {
				delivery = drivers.DeliveryNotSent
			}
		}
		return fail(code, message, result.Receipt)
	}
	// A valid envelope can contain a failed native receipt even if a caller
	// neglected to propagate its exit status. Preserve that failure.
	if r := result.Receipt; r != nil && !receiptsComplete {
		return fail("axe_input_incomplete", "AXe input did not complete; inspect the receipt before sending more input", r)
	}
	return result, nil
}

// failedBeforeInput reports AXe 2.0.4 failures that end the command before
// any HID input. Slider resolves again during post-input verification; scroll,
// batch and unknown commands can also fail after input, so they are left out.
func failedBeforeInput(command, code string) bool {
	if command != "tap" {
		return false
	}
	switch code {
	case "rejectedBeforeInput", "elementNotFound", "ambiguousElement", "elementNotHittable":
		return true
	}
	return false
}

func receiptRecoveryUncertain(r *drivers.InputReceipt) bool {
	return (r.Cleanup != "released" && r.Cleanup != "notNeeded") || nonNull(r.Quarantine)
}

func nativeReceiptDelivery(r *drivers.InputReceipt) drivers.InputDelivery {
	if receiptRecoveryUncertain(r) || r.EnqueuedEventCount < 0 {
		return drivers.DeliveryUncertain
	}
	switch r.Outcome {
	case "failedBeforeInput", "rejectedBeforeInput", "busy":
		if r.EnqueuedEventCount == 0 {
			return drivers.DeliveryNotSent
		}
	case "enqueuedAndDrained", "failedNoReplay":
	default:
		return drivers.DeliveryUncertain
	}
	if r.EnqueuedEventCount > 0 {
		return drivers.DeliverySent
	}
	return drivers.DeliveryUncertain
}

func nonNull(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

// Receipts can appear alone or nested in a broker response. Preserve their
// entire object so additions by AXe do not discard recovery evidence.
func findReceipt(raw json.RawMessage) *drivers.InputReceipt {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	if obj["outcome"] != nil && (obj["enqueuedEventCount"] != nil || obj["cleanup"] != nil) {
		var receipt drivers.InputReceipt
		if json.Unmarshal(raw, &receipt) == nil {
			receipt.Raw = append(json.RawMessage{}, raw...)
			return &receipt
		}
	}
	for _, key := range []string{"receipt", "input", "response", "result"} {
		if child := obj[key]; len(child) > 0 {
			if receipt := findReceipt(child); receipt != nil {
				return receipt
			}
		}
	}
	return nil
}
