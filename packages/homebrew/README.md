# Homebrew Tap for wipnote

Homebrew formula for [wipnote](https://github.com/shakestzd/wipnote) — local-first observability and coordination platform for AI-assisted development.

## Setup (one-time)

1. Create the GitHub tap repo: `shakestzd/homebrew-wipnote`
2. Copy `wipnote.rb` to that repo

## Usage

```bash
brew tap shakestzd/wipnote
brew install wipnote
```

## Updating

After a new release, run the update script from this directory:

```bash
./update-formula.sh 0.36.0
```

This will:
- Download the checksums file for the specified version from GitHub Releases
- Parse SHA256 values for all four platforms (darwin/linux x amd64/arm64)
- Update `wipnote.rb` in-place with the new version and correct checksums

Then commit and push the updated formula to the tap repo:

```bash
git add wipnote.rb
git commit -m "wipnote 0.36.0"
git push
```

## Formula Details

The formula installs pre-built binaries from GitHub Releases. No compilation required.

Supported platforms:
- macOS arm64 (Apple Silicon)
- macOS amd64 (Intel)
- Linux arm64
- Linux amd64

Release asset URL pattern:
```
https://github.com/shakestzd/wipnote/releases/download/go/v{VERSION}/wipnote_{VERSION}_{OS}_{ARCH}.tar.gz
```
