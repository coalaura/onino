#!/bin/sh
set -eu

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die 'Only Linux and macOS are supported.' ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	aarch64|arm64) arch=arm64 ;;
	*) die 'Only amd64 and arm64 are supported.' ;;
esac

printf 'Install CPU or GPU version? [cpu/gpu] (cpu): ' > /dev/tty

read -r variant < /dev/tty

case "$variant" in
	''|cpu|CPU)
		variant=cpu
		;;
	gpu|GPU)
		variant=gpu
		;;
	*)
		die 'Choose cpu or gpu.'
		;;
esac

temp=$(mktemp -d)

trap 'rm -rf "$temp"' EXIT
trap 'exit 1' HUP INT TERM

repo=https://github.com/coalaura/onino
release=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
version=${release##*/}
url="$repo/releases/download/$version"
asset="onino-$os-$arch-$variant"

printf 'Downloading %s (%s)...\n' "$asset" "$version"
curl -fsSL "$url/$asset" -o "$temp/$asset"
curl -fsSL "$url/SHA256SUMS" -o "$temp/SHA256SUMS"

expected=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$temp/SHA256SUMS")

if command -v sha256sum > /dev/null 2>&1; then
	actual=$(sha256sum "$temp/$asset")
else
	actual=$(shasum -a 256 "$temp/$asset")
fi

[ -n "$expected" ] && [ "${actual%% *}" = "$expected" ] || die 'Checksum verification failed.'

if [ "$(id -u)" -eq 0 ] || [ -w /usr/local/bin ]; then
	install -d /usr/local/bin
	install -m 755 "$temp/$asset" /usr/local/bin/onino
else
	sudo install -d /usr/local/bin
	sudo install -m 755 "$temp/$asset" /usr/local/bin/onino
fi

printf 'Installed onino %s (%s) to /usr/local/bin/onino.\n' "$version" "$variant"
