class Mav < Formula
  desc "Mobile Agent Verifier for iOS Bazel apps"
  homepage "https://github.com/bitomule/mav"
  version "0.0.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/bitomule/mav/releases/download/v#{version}/mav-darwin-arm64"
      sha256 "d6881612e0c3e3da51ca39165d5c4cbfaefafaac051b3bcb2e3eb1121121a36e"
    else
      url "https://github.com/bitomule/mav/releases/download/v#{version}/mav-darwin-amd64"
      sha256 "ff531122b41991823eddc602f97664c54cd6cf7ae556ab39abee68096d81d33a"
    end
  end

  def install
    bin.install cached_download => "mav"
    chmod 0755, bin/"mav"
  end

  test do
    assert_match "Mobile Agent Verifier", shell_output("#{bin}/mav")
    assert_match "cmd=doctor", shell_output("#{bin}/mav doctor")
  end
end
