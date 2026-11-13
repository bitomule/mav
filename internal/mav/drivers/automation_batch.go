package drivers

import "context"

const (
	CapSelectedInput Capability = "input.selected"
	CapKey           Capability = "keyboard"
	CapTouch         Capability = "touch"
	CapDraw          Capability = "draw"
	CapSlider        Capability = "slider"
	CapGesturePreset Capability = "gesture.preset"
	CapSettings      Capability = "device.settings"
	CapDisplays      Capability = "device.displays"
	CapReadiness     Capability = "device.readiness"
	CapDiscovery     Capability = "device.discovery"
	CapShutdown      Capability = "lifecycle.shutdown"
	CapBatch         Capability = "batch"
	CapVideoStream   Capability = "video.stream"
)

// BatchStep contains exactly one operation. It is a driver-neutral operation,
// never a command line supplied by a caller.
type BatchStep struct {
	Tap         *TapSpec
	Type        *TextSpec
	Key         *KeySpec
	KeySequence *KeySequenceSpec
	KeyCombo    *KeyComboSpec
	Touch       *TouchSpec
	Draw        *DrawSpec
	Gesture     *GestureSpec
	Swipe       *SwipeSpec
	Drag        *DragSpec
	Button      *ButtonSpec
	Slider      *SliderSpec
	SleepMs     *int
}

type BatchSpec struct {
	Steps                     []BatchStep
	ContinueOnError           bool
	AXCache, TypeSubmission   string
	TypeChunkSize             int
	WaitTimeout, PollInterval float64
	TapStyle                  string
}

type NativeBatchDriver interface {
	Driver
	Batch(context.Context, Target, BatchSpec) (NativeResult, error)
}
