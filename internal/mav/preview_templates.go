package mav

func previewBuildTemplate(bundleID string) string {
	return `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
load("@build_bazel_rules_swift//swift:swift.bzl", "swift_library")
load("//tools:shared.bzl", "versions")

swift_library(
    name = "MAVPreviewLib",
    srcs = ["PreviewHostApp.swift"],
    module_name = "MAVPreview",
    visibility = ["//visibility:private"],
    deps = [
        # Add app modules needed by previews here, for example:
        # "//Undolly:UndollyLib",
    ],
)

ios_application(
    name = "MAVPreviewApp",
    bundle_id = "` + bundleID + `",
    bundle_name = "MAVPreview",
    families = ["iphone", "ipad"],
    infoplists = ["Info.plist"],
    minimum_os_version = versions.minimum_ios_version,
    deps = [":MAVPreviewLib"],
)
`
}

func previewSwiftTemplate() string {
	return `import SwiftUI

@main
struct MAVPreviewHostApp: App {
    var body: some Scene {
        WindowGroup {
            PreviewRouter()
                .accessibilityIdentifier("mav_preview_root")
        }
    }
}

struct PreviewRouter: View {
    private var previewID: String {
        let args = CommandLine.arguments
        guard let index = args.firstIndex(of: "--mav-preview"), args.indices.contains(index + 1) else {
            return "default"
        }
        return args[index + 1]
    }

    var body: some View {
        switch previewID {
        case "default":
            DefaultPreviewView()
        default:
            MissingPreviewView(previewID: previewID)
        }
    }
}

struct DefaultPreviewView: View {
    var body: some View {
        VStack(spacing: 12) {
            Text("MAV Preview Host")
                .font(.title)
            Text("Add cases in PreviewRouter.")
                .foregroundStyle(.secondary)
        }
        .padding()
        .accessibilityIdentifier("mav_preview_default")
    }
}

struct MissingPreviewView: View {
    let previewID: String

    var body: some View {
        VStack(spacing: 12) {
            Text("Missing preview")
                .font(.title)
            Text(previewID)
                .font(.body.monospaced())
        }
        .padding()
        .accessibilityIdentifier("mav_preview_missing")
    }
}
`
}

func previewInfoPlistTemplate() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleShortVersionString</key>
	<string>1.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>UILaunchScreen</key>
	<dict/>
</dict>
</plist>
`
}
