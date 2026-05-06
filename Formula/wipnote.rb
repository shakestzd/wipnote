# Formula/wipnote.rb
#
# Homebrew formula for wipnote — local-first observability for AI-assisted development.
#
# Tap usage:
#   brew tap shakestzd/wipnote
#   brew install wipnote
#
# SHA256 values are updated automatically by the deploy pipeline for each release.
# To manually update, run: packages/homebrew/update-formula.sh VERSION

class Wipnote < Formula
  desc "Local-first observability for AI-assisted development"
  homepage "https://github.com/shakestzd/wipnote"
  version "0.39.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/shakestzd/wipnote/releases/download/v#{version}/wipnote_#{version}_darwin_arm64.tar.gz"
      sha256 "dfd39274b45be2187eb8751d6f93cbdf66a2cf2f87a3b5ca8fcb1ab35d5d044b"
    else
      url "https://github.com/shakestzd/wipnote/releases/download/v#{version}/wipnote_#{version}_darwin_amd64.tar.gz"
      sha256 "1c9e1447af74ecbfc4cda73a76fedf534e2eeb7917e02a7ae6213e7fab26ab66"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/shakestzd/wipnote/releases/download/v#{version}/wipnote_#{version}_linux_arm64.tar.gz"
      sha256 "5f265cf6d6128450b81940d515b06ea9af707da508bcd64ba835133ffcd5bf01"
    else
      url "https://github.com/shakestzd/wipnote/releases/download/v#{version}/wipnote_#{version}_linux_amd64.tar.gz"
      sha256 "8126b587a36c9cebe2e7bd2e677a46fc7eaa5c67c7e8275c837024afc02d688e"
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
