#!/usr/bin/env python3
"""Build the synthetic app into the worktree's ignored scratch directory."""
import pathlib
import plistlib
import subprocess

root = pathlib.Path(__file__).resolve().parents[2]
app = root / ".scratch" / "GoalFixture.app"
app.mkdir(parents=True, exist_ok=True)
sdk = subprocess.check_output(["xcrun", "--sdk", "iphonesimulator", "--show-sdk-path"], text=True).strip()
subprocess.run(["xcrun", "--sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-target", "arm64-apple-ios18.0-simulator", "-sdk", sdk, "-module-cache-path", str(root / ".scratch" / "swift-cache"), str(root / "tools/goal-bench/Fixture.swift"), "-o", str(app / "GoalFixture")], check=True)
with (app / "Info.plist").open("wb") as f:
    plistlib.dump({"CFBundleIdentifier": "com.example.GoalFixture", "CFBundleName": "Goal Fixture", "CFBundleExecutable": "GoalFixture", "CFBundlePackageType": "APPL", "CFBundleVersion": "1", "CFBundleShortVersionString": "1.0", "UILaunchScreen": {}, "LSRequiresIPhoneOS": True, "UIDeviceFamily": [1, 2], "MinimumOSVersion": "18.0"}, f)
subprocess.run(["codesign", "--force", "--sign", "-", str(app)], check=True)
print(app.relative_to(root))
