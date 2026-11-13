package drivers

import (
	"context"
	"encoding/json"
)

// ReferenceDriver is an optional observation/checked-input protocol. It is
// separate from an autonomous agent: callers still own planning and success.
type ReferenceDriver interface {
	Driver
	ReferenceSnapshot(context.Context, Target) (ReferenceSnapshot, error)
	ExecuteReference(context.Context, Target, ReferenceSnapshot, ReferenceOperation) (ReferenceResult, error)
}

// ReferenceTreeDriver obtains the semantic tree and its opaque references in
// one observation, for drivers whose next tree read invalidates every token.
type ReferenceTreeDriver interface {
	ReferenceDriver
	ReferenceTree(context.Context, Target) (TreeResult, ReferenceSnapshot, error)
}

type ReferenceElement struct{ Ref, Action, Role, Label, Value, ID, Frame string }
type ReferenceSnapshot struct {
	ID       string
	PID      int
	Context  json.RawMessage
	Elements []ReferenceElement
}
type ReferenceOperation struct{ Kind, Ref, Text, Direction string }
type ReferenceResult struct {
	Delivery string // not_sent, completed, partial, unknown
	Stale    bool
	Receipt  *InputReceipt
	Raw      json.RawMessage
}
