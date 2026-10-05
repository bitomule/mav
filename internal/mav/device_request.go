package mav

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// deviceRequest is the simulator a run asked for by model and OS, so the
// project config says how to get a simulator (target_command) and the caller
// says which one. `mav open --device --ios` records it on the run; every later
// command in that run hands the same pair to target_command, so a lease that
// expires is renewed on the same model instead of the config's default.
type deviceRequest struct {
	Device string `json:"device,omitempty"`
	IOS    string `json:"ios,omitempty"`
}

func (r deviceRequest) empty() bool { return r.Device == "" && r.IOS == "" }

func (r deviceRequest) env() map[string]string {
	env := map[string]string{}
	if r.Device != "" {
		env["MAV_DEVICE"] = r.Device
	}
	if r.IOS != "" {
		env["MAV_IOS"] = r.IOS
	}
	return env
}

func deviceRequestPath(run RunState) string {
	return filepath.Join(run.Dir, "device.json")
}

func writeDeviceRequest(run RunState, request deviceRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return os.WriteFile(deviceRequestPath(run), data, 0o644)
}

func readDeviceRequest(run RunState) deviceRequest {
	data, err := os.ReadFile(deviceRequestPath(run))
	if err != nil {
		return deviceRequest{}
	}
	var request deviceRequest
	if json.Unmarshal(data, &request) != nil {
		return deviceRequest{}
	}
	return request
}

// requestedDevice is the environment first, so `MAV_DEVICE=... mav ui tree`
// and the open that set them for its own process both win, then whatever the
// current run recorded.
func (c CLI) requestedDevice() deviceRequest {
	request := deviceRequest{Device: os.Getenv("MAV_DEVICE"), IOS: os.Getenv("MAV_IOS")}
	if !request.empty() {
		return request
	}
	run, err := c.resolveRun("")
	if err != nil {
		return deviceRequest{}
	}
	return readDeviceRequest(run)
}

// deviceRequestProblem refuses a request target_command would not act on. A
// model passed to a config that pins a simulator, has no target_command, or
// has one that never reads the variable would be dropped without a word and
// the run would drive some other simulator.
func deviceRequestProblem(cfg Config, request deviceRequest) map[string]string {
	example := `target_command: simpool lease --device "${MAV_DEVICE:-iPhone 17 Pro}" --os "${MAV_IOS:-26.3}"`
	if envNamesTarget() || strings.TrimSpace(cfg.SimulatorUDID) != "" {
		return map[string]string{
			"reason": "a pinned simulator wins over target_command, so --device/--ios would be ignored",
			"next":   "unset MAV_TARGET_KIND/MAV_TARGET_UDID and remove simulator_udid from .mav/config.yaml",
		}
	}
	if targetKind(cfg) == drivers.KindMac {
		return map[string]string{
			"target_kind": cfg.TargetKind,
			"reason":      "--device/--ios choose an iOS simulator and this target is a Mac app",
		}
	}
	command := strings.TrimSpace(cfg.TargetCommand)
	if command == "" {
		return map[string]string{
			"reason": "--device/--ios are handed to target_command and .mav/config.yaml has none",
			"next":   example,
		}
	}
	for name, value := range request.env() {
		if value != "" && !strings.Contains(command, name) {
			return map[string]string{
				"reason":         "target_command does not read $" + name + ", so it would be ignored",
				"target_command": command,
				"next":           example,
			}
		}
	}
	return nil
}
