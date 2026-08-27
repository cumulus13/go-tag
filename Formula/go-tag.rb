class GoTag < Formula
  desc "Colorized, safety-checked git tag management CLI"
  homepage "https://github.com/cumulus13/go-tag"
  version "1.0.1"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/cumulus13/go-tag/releases/download/v#{version}/go-tag_#{version}_darwin_amd64"
      sha256 "PUT_REAL_SHA256_DARWIN_AMD64_HERE"
    end

    on_arm do
      url "https://github.com/cumulus13/go-tag/releases/download/v#{version}/go-tag_#{version}_darwin_arm64"
      sha256 "PUT_REAL_SHA256_DARWIN_ARM64_HERE"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/cumulus13/go-tag/releases/download/v#{version}/go-tag_#{version}_linux_amd64"
      sha256 "PUT_REAL_SHA256_LINUX_AMD64_HERE"
    end

    on_arm do
      url "https://github.com/cumulus13/go-tag/releases/download/v#{version}/go-tag_#{version}_linux_arm64"
      sha256 "PUT_REAL_SHA256_LINUX_ARM64_HERE"
    end
  end

  # The release asset is named go-tag_<version>_<platform>, but it installs
  # as the `tag` command, matching the binary's own --version/--help output.
  def install
    bin.install Dir["go-tag_*"].first => "tag"
  end

  test do
    assert_match "tag", shell_output("#{bin}/tag --version")
  end
end
