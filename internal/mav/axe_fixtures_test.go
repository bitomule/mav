package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func rejectedAXeTapFixture() CommandResult {
	return CommandResult{Err: os.ErrInvalid, Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"receipt":{"outcome":"rejectedBeforeInput","enqueuedEventCount":0,"cleanup":"complete"}}},"error":{"code":"tap_failed","message":"selector was not found before input"}}`}
}

// Root CLI fixtures describe trees and input failures, rather than the AXe
// transport. Keep their payloads while exercising AXe 2's real JSON envelope.
func axeFixtureName(name string) string {
	if filepath.Base(name) == "axe" {
		return "axe"
	}
	return name
}

func axeFixtureLookup(name string, args []string) string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if axeFixtureName(name) == "axe" && arg == "--verbose" {
			continue
		}
		filtered = append(filtered, arg)
	}
	return axeFixtureName(name) + " " + strings.Join(filtered, " ")
}

func axeDeletionCount(command string) int {
	if !strings.HasPrefix(command, "axe --output json key-sequence ") {
		return 0
	}
	fields := strings.Fields(command)
	for i, field := range fields {
		if field == "--keycodes" && i+1 < len(fields) {
			codes := strings.Split(fields[i+1], ",")
			for _, code := range codes {
				if code != "42" {
					return -1
				}
			}
			return len(codes)
		}
	}
	return 0
}

func axeFixtureResult(name string, args []string, result CommandResult) CommandResult {
	if axeFixtureName(name) != "axe" {
		return result
	}
	if len(args) == 1 && args[0] == "--version" {
		if result.Err == nil && result.Stdout == "" {
			result.Stdout = "2.0.4\n"
		}
		return result
	}
	if len(args) < 2 || args[0] != "--output" || args[1] != "json" {
		return result
	}
	var existing map[string]json.RawMessage
	if json.Unmarshal([]byte(result.Stdout), &existing) == nil && existing["schema"] != nil {
		return result
	}
	var payload any = map[string]any{}
	if strings.TrimSpace(result.Stdout) != "" {
		if json.Unmarshal([]byte(result.Stdout), &payload) != nil {
			payload = result.Stdout
		}
	}
	envelope := map[string]any{"schema": "axe.output.command-result", "schemaVersion": "1", "didError": result.Err != nil || result.Code != 0, "data": map[string]any{"result": payload}}
	if result.Err != nil || result.Code != 0 {
		message := result.Stderr
		if message == "" && result.Err != nil {
			message = result.Err.Error()
		}
		envelope["error"] = map[string]string{"code": "axe_command_failed", "message": message}
	}
	raw, _ := json.Marshal(envelope)
	result.Stdout = string(raw)
	return result
}

// Never execute a real binary while an in-process CLI test supplies stdin.
func (r *sequenceRecordingRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	r.inputs = append(r.inputs, input)
	return r.Run(ctx, name, args...)
}
func (r *recordingRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	r.input = input
	return r.Run(ctx, name, args...)
}
func (f fakeRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	return f.Run(ctx, name, args...)
}
func (r errorRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	return r.Run(ctx, name, args...)
}
func (r *launchRecipeRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	return r.Run(ctx, name, args...)
}
func (r *scriptedRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	return r.Run(ctx, name, args...)
}
func (r *vmHostRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	return r.Run(ctx, name, args...)
}
