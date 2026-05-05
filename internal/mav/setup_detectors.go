package mav

import (
	"os"
	"path/filepath"
	"strings"
)

type LaunchCandidate struct {
	Source      string
	Confidence  int
	Description string
	Launch      LaunchConfig
	AppTarget   string
	BundleID    string
}

func detectLaunchCandidates(root string, cfg Config) []LaunchCandidate {
	var candidates []LaunchCandidate
	candidates = append(candidates, detectCustomLaunchCandidates(root, cfg)...)
	candidates = append(candidates, detectBazelLaunchCandidates(root, cfg)...)
	candidates = append(candidates, detectTuistLaunchCandidates(root, cfg)...)
	candidates = append(candidates, detectXcodeLaunchCandidates(root, cfg)...)
	return candidates
}

func selectLaunchCandidate(root string, cfg Config) (LaunchCandidate, bool) {
	candidates := detectLaunchCandidates(root, cfg)
	if len(candidates) == 0 {
		return LaunchCandidate{}, false
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Confidence > best.Confidence {
			best = candidate
		}
	}
	return best, true
}

func detectCustomLaunchCandidates(root string, cfg Config) []LaunchCandidate {
	var out []LaunchCandidate
	if exists(filepath.Join(root, "Makefile")) {
		data, _ := os.ReadFile(filepath.Join(root, "Makefile"))
		text := string(data)
		build := makeTargetCommand(text, "mav-build", "make mav-build")
		if build == "" {
			build = makeTargetCommand(text, "build-ios", "make build-ios")
		}
		appPath := makeTargetCommand(text, "mav-app-path", "make mav-app-path")
		if appPath == "" {
			appPath = makeTargetCommand(text, "ios-app-path", "make ios-app-path")
		}
		if build != "" || appPath != "" {
			out = append(out, LaunchCandidate{
				Source:      "custom",
				Confidence:  95,
				Description: "project Makefile MAV/iOS targets",
				Launch: LaunchConfig{Mode: "custom", Commands: LaunchCommands{
					Build:   build,
					AppPath: appPath,
					Install: targetInstallCommand(cfg),
					Launch:  targetLaunchCommand(cfg),
				}},
			})
		}
	}
	if exists(filepath.Join(root, "justfile")) || exists(filepath.Join(root, "Justfile")) {
		out = append(out, LaunchCandidate{
			Source:      "custom",
			Confidence:  90,
			Description: "project just recipes",
			Launch: LaunchConfig{Mode: "custom", Commands: LaunchCommands{
				Build:   "just mav-build",
				AppPath: "just mav-app-path",
				Install: targetInstallCommand(cfg),
				Launch:  targetLaunchCommand(cfg),
			}},
		})
	}
	return out
}

func makeTargetCommand(makefile, target, command string) string {
	if strings.Contains(makefile, "\n"+target+":") || strings.HasPrefix(makefile, target+":") {
		return command
	}
	return ""
}

func detectBazelLaunchCandidates(root string, cfg Config) []LaunchCandidate {
	target := detectAppTarget(root, cfg.ProjectName)
	if target == "" {
		return nil
	}
	targetExpr := targetShellExpr(cfg)
	return []LaunchCandidate{{
		Source:      "bazel",
		Confidence:  80,
		Description: "Bazel ios_application target",
		AppTarget:   target,
		Launch: LaunchConfig{Mode: "custom", Commands: LaunchCommands{
			Build:   "bazelisk build " + shellQuote(target),
			AppPath: "bazelisk cquery --output=files " + shellQuote(target) + " | head -1",
			Install: targetInstallCommandWithExpr(cfg, targetExpr),
			Launch:  targetLaunchCommandWithExpr(cfg, targetExpr),
		}},
	}}
}

func detectTuistLaunchCandidates(root string, cfg Config) []LaunchCandidate {
	if !exists(filepath.Join(root, "Project.swift")) && !exists(filepath.Join(root, "Workspace.swift")) {
		return nil
	}
	scheme := cfg.ProjectName
	if scheme == "" {
		scheme = filepath.Base(root)
	}
	return []LaunchCandidate{{
		Source:      "tuist",
		Confidence:  65,
		Description: "Tuist project manifest",
		Launch: LaunchConfig{Mode: "custom", Commands: LaunchCommands{
			Build:   "tuist generate && tuist xcodebuild build -scheme " + shellQuote(scheme) + " -destination " + shellQuote(xcodeDestination(cfg)),
			AppPath: `find "$MAV_ROOT" -path "*.app" -type d | head -1`,
			Install: targetInstallCommand(cfg),
			Launch:  targetLaunchCommand(cfg),
		}},
	}}
}

func detectXcodeLaunchCandidates(root string, cfg Config) []LaunchCandidate {
	matches, _ := filepath.Glob(filepath.Join(root, "*.xcworkspace"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(root, "*.xcodeproj"))
	}
	if len(matches) == 0 {
		return nil
	}
	project := matches[0]
	scheme := cfg.ProjectName
	if scheme == "" {
		scheme = strings.TrimSuffix(filepath.Base(project), filepath.Ext(project))
	}
	projectFlag := "-project"
	if filepath.Ext(project) == ".xcworkspace" {
		projectFlag = "-workspace"
	}
	return []LaunchCandidate{{
		Source:      "xcode",
		Confidence:  60,
		Description: "Xcode project/workspace",
		Launch: LaunchConfig{Mode: "custom", Commands: LaunchCommands{
			Build:   "xcodebuild " + projectFlag + " " + shellQuote(project) + " -scheme " + shellQuote(scheme) + " -destination " + shellQuote(xcodeDestination(cfg)) + " build",
			AppPath: xcodeAppPathCommand(cfg),
			Install: targetInstallCommand(cfg),
			Launch:  targetLaunchCommand(cfg),
		}},
	}}
}

func targetShellExpr(cfg Config) string {
	if isDeviceTarget(cfg) {
		return `"$MAV_DEVICE_ID"`
	}
	if cfg.SimulatorUDID != "" {
		return shellQuote(cfg.SimulatorUDID)
	}
	return `"$MAV_UDID"`
}

func targetInstallCommand(cfg Config) string {
	return targetInstallCommandWithExpr(cfg, targetShellExpr(cfg))
}

func targetInstallCommandWithExpr(cfg Config, targetExpr string) string {
	if isDeviceTarget(cfg) {
		return "xcrun devicectl device install app --device " + targetExpr + ` "$MAV_APP_PATH"`
	}
	return "xcrun simctl install " + targetExpr + ` "$MAV_APP_PATH"`
}

func targetLaunchCommand(cfg Config) string {
	return targetLaunchCommandWithExpr(cfg, targetShellExpr(cfg))
}

func targetLaunchCommandWithExpr(cfg Config, targetExpr string) string {
	if isDeviceTarget(cfg) {
		return "xcrun devicectl device process launch --device " + targetExpr + ` --terminate-existing "$MAV_BUNDLE_ID"`
	}
	return "xcrun simctl launch " + targetExpr + ` "$MAV_BUNDLE_ID"`
}

func xcodeDestination(cfg Config) string {
	if isDeviceTarget(cfg) {
		if cfg.DeviceIdentifier != "" {
			return "platform=iOS,id=" + cfg.DeviceIdentifier
		}
		return "generic/platform=iOS"
	}
	return "platform=iOS Simulator,id=$MAV_UDID"
}

func xcodeAppPathCommand(cfg Config) string {
	if isDeviceTarget(cfg) {
		return `find "$HOME/Library/Developer/Xcode/DerivedData" -path "*/Build/Products/*-iphoneos/*.app" -type d | head -1`
	}
	return `find "$HOME/Library/Developer/Xcode/DerivedData" -path "*/Build/Products/*-iphonesimulator/*.app" -type d | head -1`
}
