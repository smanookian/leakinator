#!/bin/sh
# Installs leakinator and what it needs: ffmpeg and tesseract (with English).
# Works on Arch/Omarchy, Debian/Ubuntu, Fedora and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/smanookian/leakinator/main/install.sh | sh
#
# Options (environment variables):
#   LEAKINATOR_DIR=/some/bin    install folder (default ~/.local/bin)
#   LEAKINATOR_VERSION=0.1.0    version (default: the latest release)
set -eu

repo=smanookian/leakinator
dir=${LEAKINATOR_DIR:-$HOME/.local/bin}

say() { printf '%s\n' "$*"; }
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }
as_root() {
	if [ "$(id -u)" = 0 ]; then
		"$@"
	else
		have sudo || fail "sudo is needed to run: $*"
		sudo "$@"
	fi
}
deps_ok() {
	have ffmpeg && have ffprobe && have tesseract &&
		tesseract --list-langs 2>&1 | grep -qx eng
}

have curl || fail "curl is needed."
have tar || fail "tar is needed."

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "This script is for Linux and macOS. On Windows, see the README." ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "No build for $(uname -m)." ;;
esac

# 1. ffmpeg and tesseract
if deps_ok; then
	say "✓ ffmpeg and tesseract are installed"
else
	say "Installing ffmpeg and tesseract…"
	if [ "$os" = darwin ]; then
		have brew || fail "Install Homebrew first (https://brew.sh), then run this again."
		brew install ffmpeg tesseract
	elif have pacman; then
		as_root pacman -S --needed --noconfirm ffmpeg tesseract tesseract-data-eng
	elif have apt-get; then
		as_root apt-get update
		as_root apt-get install -y ffmpeg tesseract-ocr tesseract-ocr-eng
	elif have dnf; then
		as_root dnf install -y ffmpeg tesseract tesseract-langpack-eng ||
			fail "On Fedora, ffmpeg comes from RPM Fusion (https://rpmfusion.org). Add it, then run this again."
	else
		fail "Install ffmpeg and tesseract (with English) yourself, then run this again."
	fi
	deps_ok || fail "ffmpeg or tesseract (English) is still missing."
	say "✓ ffmpeg and tesseract installed"
fi

# 2. leakinator
ver=${LEAKINATOR_VERSION:-}
if [ -z "$ver" ]; then
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "Can't reach GitHub."
	ver=${latest##*/v}
fi
file="leakinator_${ver}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/v$ver"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading leakinator $ver ($os/$arch)…"
curl -fsSL -o "$tmp/$file" "$base/$file" || fail "Download failed: $base/$file"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "Download failed: checksums.txt"

want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
if have sha256sum; then
	got=$(sha256sum "$tmp/$file" | awk '{ print $1 }')
else
	got=$(shasum -a 256 "$tmp/$file" | awk '{ print $1 }')
fi
[ -n "$want" ] && [ "$want" = "$got" ] || fail "Checksum does not match. Not installing."
say "✓ Checksum OK"

mkdir "$tmp/x"
tar -xzf "$tmp/$file" -C "$tmp/x"
[ -f "$tmp/x/leakinator" ] || fail "No leakinator in $file."
mv "$tmp/x/leakinator" "$tmp/leakinator"
chmod 755 "$tmp/leakinator"
if mkdir -p "$dir" 2>/dev/null && [ -w "$dir" ]; then
	mv "$tmp/leakinator" "$dir/leakinator"
else
	as_root mkdir -p "$dir"
	as_root mv "$tmp/leakinator" "$dir/leakinator"
fi

# 3. Check
"$dir/leakinator" --version >/dev/null || fail "Installed, but $dir/leakinator does not run."
say "✓ Installed $("$dir/leakinator" --version) to $dir/leakinator"

case ":$PATH:" in
*":$dir:"*)
	found=$(command -v leakinator)
	[ "$found" = "$dir/leakinator" ] || say "Note: $found comes first in your PATH. Remove it or use $dir/leakinator."
	;;
*)
	say ""
	say "$dir is not in your PATH. Add it with:"
	say "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.bashrc   (then open a new terminal)"
	;;
esac
say ""
say "Try it:  leakinator video.mp4 --env .env"
