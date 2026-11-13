package drivers

import (
	"context"
	"encoding/json"
	"fmt"
)

// InputContext binds selected input to an explicitly observed display and app.
// A zero context uses the driver's ordinary route. It never implies agent mode.
type InputContext struct {
	Selected    bool    `json:"selected,omitempty" yaml:"selected,omitempty"`
	ScreenID    *uint32 `json:"screenId,omitempty" yaml:"screenId,omitempty"`
	ScreenUUID  string  `json:"screenUuid,omitempty" yaml:"screenUuid,omitempty"`
	ExpectedPID int32   `json:"expectedPid,omitempty" yaml:"expectedPid,omitempty"`
}

type InputDelivery string

const (
	DeliveryNotSent   InputDelivery = "notSent"
	DeliverySent      InputDelivery = "sent"
	DeliveryUncertain InputDelivery = "uncertain"
)

// InputReceipt preserves the native evidence needed to decide whether further
// input is safe. Raw contains the complete receipt, including partial results.
type InputReceipt struct {
	Outcome            string          `json:"outcome,omitempty"`
	EnqueuedEventCount int             `json:"enqueuedEventCount,omitempty"`
	Cleanup            string          `json:"cleanup,omitempty"`
	Quarantine         json.RawMessage `json:"quarantine,omitempty"`
	Raw                json.RawMessage `json:"-"`
}

type NativeResult struct {
	Data     json.RawMessage `json:"data,omitempty"`
	Receipt  *InputReceipt   `json:"receipt,omitempty"`
	Envelope json.RawMessage `json:"-"`
}

// DriverError is evidence, not a request to retry. Without a native receipt a
// lost/malformed response from a mutation has uncertain delivery.
type DriverError struct {
	Driver   string
	Code     string
	Message  string
	Delivery InputDelivery
	Receipt  *InputReceipt
	Details  json.RawMessage
}

func (e *DriverError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
func (e *DriverError) InputWasSent() bool { return e.Delivery != DeliveryNotSent }

type KeySpec struct {
	Code       int
	DurationMs int
	FocusID    string
}
type KeySequenceSpec struct {
	Codes   []int
	DelayMs *int
	FocusID string
}
type KeyComboSpec struct {
	Modifiers []int
	Key       int
	FocusID   string
}
type TouchSpec struct {
	X, Y         int
	Down, Up     bool
	DelayMs      *int
	ContactToken string
}
type DrawSpec struct {
	Points                  []PathPoint
	DurationMs              int
	Close                   bool
	PreDelayMs, PostDelayMs int
}
type SliderSpec struct {
	Selector                  ElementSelector
	ElementType               string
	Percent                   *float64
	Endpoint                  string
	WaitTimeout, PollInterval float64
}
type GestureSpec struct {
	Name                                       string
	X, Y, X2, Y2                               *int
	EndX, EndY, EndX2, EndY2                   *int
	Radius, Scale, Degrees, Distance           *float64
	HoldMs, IntervalMs, EdgeInset              *int
	DurationMs, Delta, PreDelayMs, PostDelayMs int
	UntilID, UntilLabel, InsideID, InsideLabel string
	MaxSwipes, ScreenWidth, ScreenHeight       int
}
type SettingsSpec struct {
	Appearance, TextSize, LiquidGlass, ColorFilter                             string
	IncreaseContrast, ReduceMotion, ReduceTransparency, ShowBorders, VoiceOver *bool
	LiquidGlassOpacity, ColorFilterIntensity                                   *float64
}
type LocationSpec struct {
	Action              string
	Latitude, Longitude *float64
	Scenario            string
}
type StatusBarActionSpec struct {
	Action, Preset string
	Values         StatusBarSpec
}
type ClipboardSpec struct {
	Text     string
	Readback bool
}
type ButtonSpec struct {
	Button     HardwareButton
	DurationMs int
	AllowLock  bool
}
type RotationSpec struct {
	Direction string
	Timeout   float64
}

// NativeAutomationDriver exposes ordinary, provider-free automation operations.
// Support is target-dependent; unsupported operations fail before sending input.
// Snapshot/ref protocols and autonomous agents are deliberately separate APIs.
type NativeAutomationDriver interface {
	Driver
	Key(context.Context, Target, KeySpec) (NativeResult, error)
	KeySequence(context.Context, Target, KeySequenceSpec) (NativeResult, error)
	KeyCombo(context.Context, Target, KeyComboSpec) (NativeResult, error)
	Touch(context.Context, Target, TouchSpec) (NativeResult, error)
	Draw(context.Context, Target, DrawSpec) (NativeResult, error)
	Slider(context.Context, Target, SliderSpec) (NativeResult, error)
	Gesture(context.Context, Target, GestureSpec) (NativeResult, error)
	Settings(context.Context, Target, SettingsSpec) (NativeResult, error)
	Location(context.Context, Target, LocationSpec) (NativeResult, error)
	StatusBar(context.Context, Target, StatusBarActionSpec) (NativeResult, error)
	Clipboard(context.Context, Target, ClipboardSpec) (NativeResult, error)
	Button(context.Context, Target, ButtonSpec) (NativeResult, error)
	Turn(context.Context, Target, RotationSpec) (NativeResult, error)
	Displays(context.Context, Target) (NativeResult, error)
	Readiness(context.Context, Target) (NativeResult, error)
	Discover(context.Context) (NativeResult, error)
	Shutdown(context.Context, Target) (NativeResult, error)
}
