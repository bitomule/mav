class Mav < Formula
  desc "Mobile Agent Verifier for iOS Bazel apps"
  homepage "https://github.com/bitomule/mav"
  version "0.0.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/bitomule/mav/releases/download/v#{version}/mav-darwin-arm64"
      sha256 "c5aa187a6671095cf6bb13fd0c2757c3f9c2742af51bc830693f3d6269464055"
    else
      url "https://github.com/bitomule/mav/releases/download/v#{version}/mav-darwin-amd64"
      sha256 "1dfda80d01dd288f031723db163a68ff56dcadee3ecbf150b243cc43d2e256e5"
    end
  end

  def install
    bin.install cached_download => "mav"
  end

  test do
    assert_match "Mobile Agent Verifier", shell_output("#{bin}/mav")
    assert_match "cmd=doctor", shell_output("#{bin}/mav doctor")
  end
end
