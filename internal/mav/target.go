package mav

func normalizedTargetKind(cfg Config) string {
	if cfg.TargetKind == "device" {
		return "device"
	}
	return "simulator"
}

func isPhysicalDevice(cfg Config) bool {
	return normalizedTargetKind(cfg) == "device"
}

func targetUDID(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return cfg.DeviceUDID
	}
	return cfg.SimulatorUDID
}

func targetName(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return cfg.DeviceName
	}
	return cfg.SimulatorName
}

func targetRuntime(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return ""
	}
	return cfg.SimulatorRuntime
}

// OK is the CLI-bound counterpart of the package-level OK: it's the single
// place a command's success fields pick up which simulator or device they
// actually acted on. Route every success output through c.OK (not the bare
// OK) so nobody has to remember to add the field by hand -- in hot-path
// usage (an agent driving mav command-by-command, not just via `mav run`)
// that field is how the next call knows which target to keep using instead
// of guessing; guessing wrong with several agents on one machine means
// silently driving someone else's simulator while taps and assertions keep
// passing.
func (c CLI) OK(cmd string, fields map[string]string) Output {
	return OK(cmd, c.withResolvedTarget(fields))
}

// withResolvedTarget fills udid/target_kind/target_name into fields, unless
// the caller already set udid explicitly (e.g. sim.select reporting the
// simulator it just picked). Most project configs no longer pin
// simulator_udid (see config.go's MAV_TARGET_KIND/MAV_TARGET_UDID handling),
// so most commands actually run against "whatever simulator is booted" --
// resolving that concretely here, instead of leaving it implicit, is the
// point: it turns a silent default into something the caller can read off
// the very first response.
func (c CLI) withResolvedTarget(fields map[string]string) map[string]string {
	if fields == nil {
		fields = map[string]string{}
	}
	if _, ok := fields["udid"]; ok {
		return fields
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return fields
	}
	udid := targetUDID(cfg)
	name := targetName(cfg)
	kind := normalizedTargetKind(cfg)
	if udid == "" && kind == "simulator" && c.Runner != nil {
		// No pinned or env-provided UDID: the underlying tools were told
		// "booted" and resolved it themselves without mav ever learning
		// which simulator that was. Ask once, explicitly, so the report is
		// honest instead of blank.
		udid, name, _ = detectBootedSimulator(c.Runner)
	}
	if udid == "" {
		return fields
	}
	fields["udid"] = udid
	if _, ok := fields["target_kind"]; !ok {
		fields["target_kind"] = kind
	}
	if name != "" {
		if _, ok := fields["target_name"]; !ok {
			fields["target_name"] = name
		}
	}
	return fields
}
