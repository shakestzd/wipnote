class Wipnote < Formula
  desc "Local-first observability and coordination platform for AI-assisted development"
  homepage "https://github.com/shakestzd/wipnote"
  version "0.35.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/shakestzd/wipnote/releases/download/go/v#{version}/wipnote_#{version}_darwin_arm64.tar.gz"
      sha256 "SHA256_DARWIN_ARM64"
    else
      url "https://github.com/shakestzd/wipnote/releases/download/go/v#{version}/wipnote_#{version}_darwin_amd64.tar.gz"
      sha256 "SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/shakestzd/wipnote/releases/download/go/v#{version}/wipnote_#{version}_linux_arm64.tar.gz"
      sha256 "SHA256_LINUX_ARM64"
    else
      url "https://github.com/shakestzd/wipnote/releases/download/go/v#{version}/wipnote_#{version}_linux_amd64.tar.gz"
      sha256 "SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "wipnote"
    bin.install_symlink "wipnote" => "wn"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/wipnote version")
  end
end
