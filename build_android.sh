#!/usr/bin/env bash
# Custom script to build Tailscale for Magisk Tailscaled

set -euox pipefail

# If $ANDROID_NDK_PATH is not set, use the default path
if [ -z "${ANDROID_NDK_PATH:-}" ]; then
   ANDROID_NDK_PATH="/tmp/android-ndk-r27c-linux/toolchains/llvm/prebuilt/linux-x86_64/bin"
fi
# Specify the Android NDK path
if [ ! -d "$ANDROID_NDK_PATH" ]; then
    echo "Android NDK path not found: $ANDROID_NDK_PATH" # ANDROID_NDK_PATH="/tmp/android-ndk-r27c-linux/toolchains/llvm/prebuilt/linux-x86_64/bin"
    # exit 1
    curl -L https://dl.google.com/android/repository/android-ndk-r27c-linux.zip -o /tmp/android-ndk-r27c-linux.zip
    unzip -q /tmp/android-ndk-r27c-linux.zip -d /tmp
    mv /tmp/android-ndk-r27c /tmp/android-ndk-r27c-linux
    rm /tmp/android-ndk-r27c-linux.zip
    export ANDROID_NDK_PATH="/tmp/android-ndk-r27c-linux/toolchains/llvm/prebuilt/linux-x86_64/bin"
    echo "Android NDK path set to: $ANDROID_NDK_PATH"
fi


# export TMPDIR=${TMPDIR:-/tmp}
# export GOTMPDIR="$TMPDIR/go-build"
# export GOCACHE="$TMPDIR/.gocache"
# export GOMODCACHE="$TMPDIR/.gomodcache"
# export XDG_CACHE_HOME="$TMPDIR/.cache"
# export XDG_HOME_DIR="$TMPDIR/.xdg"
# export HOME="$TMPDIR"
# export GOCROSS_NO_GO_INSTALL=1

# mkdir -p "$GOTMPDIR" "$GOCACHE" "$GOMODCACHE" "$XDG_CACHE_HOME"
# Use the "go" binary from the "tool" directory (which is github.com/tailscale/go)
# export PATH="$PWD"/tool:"$PATH"

export TS_USE_TOOLCHAIN=1
# export GOROOT=$(./tool/go env GOROOT)
eval "$(./build_dist.sh shellvars)"
export PATH="$ANDROID_NDK_PATH:$PATH"
# $GOROOT/bin/go version
# command -v go
# which go
# go version

# Parse arguments
PRE_RELEASE=""
POSITIONAL_ARGS=()

while [ "$#" -gt 0 ]; do
    case "$1" in
        --pre)
            PRE_RELEASE="1"
            shift
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

# Restore positional parameters
set -- "${POSITIONAL_ARGS[@]}"

if [ "$#" -eq 0 ]; then
    echo "Usage: $0 [--pre] <arm|arm64>"
    exit 1
fi

# Add -pre suffix to version if --pre flag is set
if [ -n "$PRE_RELEASE" ]; then
    VERSION_SHORT="${VERSION_SHORT}-pre"
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
go build -tags="ts_include_cli,ts_omit_aws,ts_omit_bird,ts_omit_tap,ts_omit_kube,ts_omit_completion,ts_omit_wakeonlan,ts_omit_capture" \
    --ldflags="$ldflags" \
    -o ./dist/tailscaled.$GOARCH \
    -trimpath ./cmd/tailscaled

chmod +x ./dist/tailscaled.$GOARCH
echo "Build completed: $(file ./dist/tailscaled.$GOARCH)"
echo "File size: $(du -h ./dist/tailscaled.$GOARCH | cut -f1)"