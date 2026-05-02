package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Simulator struct {
	UDID    string
	Name    string
	Runtime string
	State   string
}

func ListSimulators(runner Runner) ([]Simulator, error) {
	result := runner.Run(context.Background(), "xcrun", "simctl", "list", "devices", "-j")
	if result.Err != nil {
		return nil, fmt.Errorf("%s", firstLine(result.Stderr))
	}
	var parsed struct {
		Devices map[string][]struct {
			UDID        string `json:"udid"`
			Name        string `json:"name"`
			State       string `json:"state"`
			IsAvailable bool   `json:"isAvailable"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return nil, err
	}
	var sims []Simulator
	for runtime, devices := range parsed.Devices {
		for _, device := range devices {
			if !device.IsAvailable {
				continue
			}
			sims = append(sims, Simulator{
				UDID:    device.UDID,
				Name:    device.Name,
				Runtime: runtime,
				State:   device.State,
			})
		}
	}
	return sims, nil
}

func SelectSimulator(sims []Simulator, name, runtime, udid string) (Simulator, bool) {
	if udid != "" {
		for _, sim := range sims {
			if sim.UDID == udid {
				return sim, true
			}
		}
		return Simulator{}, false
	}
	var best Simulator
	bestScore := -1
	for _, sim := range sims {
		score := 0
		if name == "" || strings.Contains(strings.ToLower(sim.Name), strings.ToLower(name)) {
			score += 10
		} else {
			continue
		}
		if runtime == "" || strings.Contains(strings.ToLower(sim.Runtime), strings.ToLower(runtime)) {
			score += 10
		} else {
			continue
		}
		if sim.State == "Booted" {
			score += 5
		}
		if score > bestScore {
			best = sim
			bestScore = score
		}
	}
	return best, bestScore >= 0
}
