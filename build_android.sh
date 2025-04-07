#!/usr/bin/env sh
# Custom script to build Tailscale for Magisk Tailscaled

set -eu

# Specify the Android NDK path
if [ ! -d "$ANDROID_NDK_PATH" ]; then
    echo "Android NDK path not found: $ANDROID_NDK_PATH" // ANDROID_NDK_PATH="/tmp/android-ndk-r27c/toolchains/llvm/prebuilt/linux-x86_64/bin"
    exit 1
fi

export PATH="$ANDROID_NDK_PATH:$PATH"

# Use the "go" binary from the "tool" directory (which is github.com/tailscale/go)
# export PATH="$PWD"/tool:"$PATH"

eval "$(./build_dist.sh shellvars)"

if [ "$#" -eq 0 ]; then
    echo "Usage: $0 <arm|arm64>"
    exit 1
fi

# Set the target architecture and platform
case "$1" in
    arm)
        export CC=armv7a-linux-androideabi21-clang
        export CXX=armv7a-linux-androideabi21-clang++
        export GOARCH="arm"
        ;;
    arm64)
        export CC=aarch64-linux-android21-clang
        export CXX=aarch64-linux-android21-clang++
        export GOARCH="arm64"
        ;;
    amd64)
        export CC=x86_64-linux-android21-clang
        export CXX=x86_64-linux-android21-clang++
        export GOARCH="amd64"
        ;;
    *)
        echo "Unknown option: $1"
        exit 1
        ;;
esac

# Set common environment variables
export GOOS=android
export CGO_ENABLED=1
ldflags="-X tailscale.com/version.longStamp=${VERSION_LONG} -X tailscale.com/version.shortStamp=${VERSION_SHORT} -X tailscale.com/version.gitCommitStamp=${VERSION_GIT_HASH}"
ldflags="$ldflags -w -s"
# Build the binary
go build -tags="ts_include_cli,ts_omit_aws,ts_omit_bird,ts_omit_tap,ts_omit_kube,ts_omit_completion,ts_omit_ssh,ts_omit_wakeonlan,ts_omit_capture" \
    --ldflags="$ldflags" \
    -o ./dist/tailscale.combined.$GOARCH \
    -trimpath ./cmd/tailscaled

echo "Build completed: $(file ./dist/tailscale.combined.$GOARCH)"
echo "File size: $(du -h ./dist/tailscale.combined.$GOARCH | cut -f1)"
