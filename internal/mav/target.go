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
