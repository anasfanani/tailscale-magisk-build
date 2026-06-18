#!/usr/bin/env bash
# Tailscale Android development script
# Usage:
#   ./scripts/android.sh build [--pre] [--upx] [--nocgo] <arm|arm64|amd64>
#   ./scripts/android.sh check [arm64]
#   ./scripts/android.sh compat [--check] [--build] [stop-tag]
#   ./scripts/android.sh update [--dry-run] [target-tag]

set -euo pipefail

NDK_VERSION="r27c"
NDK_DIR="/tmp/android-ndk-${NDK_VERSION}-linux"

# --- Functions ---

setup_ndk() {
    export ANDROID_NDK_PATH="${ANDROID_NDK_PATH:-${NDK_DIR}/toolchains/llvm/prebuilt/linux-x86_64/bin}"
    if [ -d "$ANDROID_NDK_PATH" ]; then return; fi
    echo "Downloading NDK ${NDK_VERSION}..."
    curl -# -L "https://dl.google.com/android/repository/android-ndk-${NDK_VERSION}-linux.zip" -o /tmp/android-ndk.zip
    unzip -q /tmp/android-ndk.zip -d /tmp
    mv "/tmp/android-ndk-${NDK_VERSION}" "$NDK_DIR"
    rm /tmp/android-ndk.zip
}

set_arch() {
    export GOOS=android
    case "$1" in
        arm)   export GOARCH=arm CC=armv7a-linux-androideabi21-clang CXX=armv7a-linux-androideabi21-clang++ ;;
        arm64) export GOARCH=arm64 CC=aarch64-linux-android21-clang CXX=aarch64-linux-android21-clang++ ;;
        amd64) export GOARCH=amd64 CC=x86_64-linux-android21-clang CXX=x86_64-linux-android21-clang++ ;;
        *)     echo "Unknown arch: $1"; exit 1 ;;
    esac
}

get_build_tags() {
    local remove="aws,bird,tap,kube,completion,completion_scripts,wakeonlan,capture,systray,syspolicy,appconnectors,identityfederation,usermetrics,logtail,netlog,linuxdnsfight,syspolicy,tpm"
    GOOS= GOARCH= ./tool/go run ./cmd/featuretags --remove "$remove" --add "cli"
}

get_ldflags() {
    eval "$(./build_dist.sh shellvars)"
    if [ "${PRE_RELEASE:-}" = "1" ]; then
        VERSION_SHORT="${VERSION_SHORT}-pre"
    fi
    echo "-X tailscale.com/version.longStamp=${VERSION_LONG} -X tailscale.com/version.shortStamp=${VERSION_SHORT} -X tailscale.com/version.gitCommitStamp=${VERSION_GIT_HASH} -w -s"
}

compress() {
    if ! command -v upx &>/dev/null; then
        curl -# -L "https://github.com/upx/upx/releases/download/v5.0.2/upx-5.0.2-amd64_linux.tar.xz" -o /tmp/upx.tar.xz
        tar -xf /tmp/upx.tar.xz -C /tmp && sudo mv /tmp/upx-5.0.2-amd64_linux/upx /usr/local/bin/
        rm -rf /tmp/upx.tar.xz /tmp/upx-5.0.2-amd64_linux
    fi
    echo "Before: $(du -h "$1" | cut -f1)"
    upx --lzma --best "$1" 2>&1 | grep -v "^$" || true
    echo "After:  $(du -h "$1" | cut -f1)"
}

# --- Commands ---

cmd_check() {
    local arch="${1:-arm64}"
    set_arch "$arch"
    export CGO_ENABLED=0
    local tags=$(get_build_tags)
    echo "Checking android/$GOARCH..."
    ./tool/go vet -tags="$tags" ./cmd/tailscaled ./cmd/tailscale
    ./tool/go build -tags="$tags" -o /dev/null -trimpath ./cmd/tailscaled
    ./tool/go build -tags="$tags" -o /dev/null -trimpath ./cmd/tailscale
    echo "✓ OK"
}

cmd_build() {
    local PRE_RELEASE="" USE_UPX="" NO_CGO=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --pre)   PRE_RELEASE="1"; shift ;;
            --upx)   USE_UPX="1"; shift ;;
            --nocgo) NO_CGO="1"; shift ;;
            *)       break ;;
        esac
    done
    [ "$#" -eq 0 ] && { echo "Usage: $0 build [--pre] [--upx] [--nocgo] <arm|arm64|amd64>"; exit 1; }

    set_arch "$1"
    if [ -z "$NO_CGO" ]; then
        export CGO_ENABLED=1
        setup_ndk
        export PATH="$ANDROID_NDK_PATH:$PATH"
    else
        export CGO_ENABLED=0
    fi

    local tags=$(get_build_tags)
    local ldflags=$(get_ldflags)
    mkdir -p ./dist

    ./tool/go build -tags="$tags" -ldflags="$ldflags" -o "./dist/tailscaled.${GOARCH}" -trimpath ./cmd/tailscaled
    chmod +x "./dist/tailscaled.${GOARCH}"
    echo "Built: dist/tailscaled.${GOARCH} ($(du -h "./dist/tailscaled.${GOARCH}" | cut -f1))"

    if [ -n "$USE_UPX" ]; then compress "./dist/tailscaled.${GOARCH}"; fi
}

cmd_compat() {
    local to="" do_check="" do_build=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --check) do_check="1"; shift ;;
            --build) do_build="1"; shift ;;
            *)       to="$1"; shift ;;
        esac
    done

    local from="v$(cat VERSION.txt)"

    local repo_root
    repo_root="$(git rev-parse --show-toplevel)"
    local tmp="/tmp/tailscale-compat-$$"

    echo "Base: $from (from VERSION.txt)"
    echo "Cloning to $tmp..."
    git clone --quiet --shared "$repo_root" "$tmp"
    cd "$tmp"

    if ! git tag | grep -q "^v1.9"; then
        git remote add upstream https://github.com/tailscale/tailscale.git 2>/dev/null
        git fetch upstream --tags --quiet
    fi

    # Find base commit and squash android patch
    local base_commit
    base_commit=$(git log --oneline | grep "VERSION.txt: this is v\?${from#v}" | head -1 | cut -d' ' -f1)
    if [ -z "$base_commit" ]; then
        echo "Cannot find base commit for $from"
        rm -rf "$tmp"
        exit 1
    fi

    git checkout -q -b android-patch HEAD
    git reset --soft "$base_commit"
    git commit -q -m "android patch" --allow-empty

    local patch_commit
    patch_commit=$(git rev-parse HEAD)

    # Get upstream-only tags (exclude android/pre custom tags)
    local tags
    if [ -n "$to" ]; then
        tags=$(git tag -l 'v[0-9]*' --sort=version:refname | grep -v "android" | awk -v f="$from" -v t="$to" 'f<$0 && $0<=t')
    else
        tags=$(git tag -l 'v[0-9]*' --sort=version:refname | grep -v "android" | awk -v f="$from" '$0>f' | head -30)
    fi

    if [ -z "$tags" ]; then
        echo "No tags found after $from"
        rm -rf "$tmp"
        exit 1
    fi

    echo ""
    local passed=0 failed=0
    while IFS= read -r tag; do
        git checkout -q "$tag" 2>/dev/null
        git checkout -q -b "test-$tag"
        if git cherry-pick "$patch_commit" --quiet 2>/dev/null; then
            local status="✓ $tag"
            if [ -n "$do_check" ] || [ -n "$do_build" ]; then
                local btags
                btags=$(get_build_tags 2>/dev/null)
                if [ -n "$do_check" ]; then
                    if GOOS=android GOARCH=arm64 CGO_ENABLED=0 ./tool/go vet -tags="$btags" ./cmd/tailscaled ./cmd/tailscale 2>/dev/null; then
                        status="$status [vet ✓]"
                    else
                        status="$status [vet ✗]"
                    fi
                fi
                if [ -n "$do_build" ]; then
                    if GOOS=android GOARCH=arm64 CGO_ENABLED=0 ./tool/go build -tags="$btags" -o /dev/null -trimpath ./cmd/tailscaled 2>/dev/null; then
                        status="$status [build ✓]"
                    else
                        status="$status [build ✗]"
                    fi
                fi
            fi
            echo "$status"
            passed=$((passed + 1))
        else
            local conflicts
            conflicts=$(git diff --name-only --diff-filter=U 2>/dev/null | tr '\n' ' ')
            echo "✗ $tag → $conflicts"
            failed=$((failed + 1))
            git cherry-pick --abort 2>/dev/null
        fi
        git checkout -q "$tag" 2>/dev/null
        git branch -q -D "test-$tag" 2>/dev/null
    done <<< "$tags"

    echo ""
    echo "Passed: $passed  Failed: $failed"
    rm -rf "$tmp"
}

cmd_update() {
    local target="${1:-}" dry_run=""
    if [ "$target" = "--dry-run" ]; then
        dry_run="1"; target="${2:-}"
    fi

    local from="v$(cat VERSION.txt)"
    local repo_root
    repo_root="$(git rev-parse --show-toplevel)"
    local original_branch
    original_branch=$(git rev-parse --abbrev-ref HEAD)

    # Ensure upstream remote
    if ! git remote get-url upstream &>/dev/null; then
        git remote add upstream https://github.com/tailscale/tailscale.git
    fi
    git fetch upstream --tags --quiet

    # Determine target
    if [ -z "$target" ]; then
        target=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-version:refname | grep -v -- '-' | head -1)
    fi
    if [[ ! "$target" =~ ^v ]]; then target="v${target}"; fi

    if ! git rev-parse "$target" >/dev/null 2>&1; then
        echo "Tag $target not found"; exit 1
    fi

    if [ "$from" = "$target" ]; then
        echo "Already at $target"; exit 0
    fi

    echo "Update: $from → $target"

    # Find base commit
    local base_commit
    base_commit=$(git log --oneline | grep "VERSION.txt: this is v\?${from#v}" | head -1 | cut -d' ' -f1)
    if [ -z "$base_commit" ]; then
        echo "Cannot find base commit for $from"; exit 1
    fi

    if [ -n "$dry_run" ]; then
        echo "[dry-run] Would cherry-pick android patches onto $target"
        local tmp="/tmp/tailscale-update-$$"
        git clone --quiet --shared "$repo_root" "$tmp"
        cd "$tmp"
        git checkout -q -b patch HEAD
        git reset --soft "$base_commit"
        git commit -q -m "android patch"
        local pc=$(git rev-parse HEAD)
        git checkout -q "$target"
        git checkout -q -b test
        if git cherry-pick "$pc" --quiet 2>/dev/null; then
            echo "✓ Would apply cleanly"
        else
            echo "✗ Would have conflicts:"
            git diff --name-only --diff-filter=U 2>/dev/null | sed 's/^/  /'
            git cherry-pick --abort
        fi
        rm -rf "$tmp"
        return
    fi

    # Work on a temp branch to create the squashed patch
    local new_branch="${target#v}-android-dev"
    if git rev-parse --verify "$new_branch" >/dev/null 2>&1; then
        echo "Branch $new_branch already exists. Delete it first or use a different target."
        exit 1
    fi

    # Create temp branch from current HEAD, squash patches there
    local short_head
    short_head=$(git rev-parse --short HEAD)
    local commit_list
    commit_list=$(git log --format="  %h %s" "$base_commit"..HEAD --reverse)
    local coauthors
    coauthors=$(git log --format="Co-authored-by: %an <%ae>" "$base_commit"..HEAD | sort -u)
    local commit_msg="feat: android modifications

Updated from $from to $target
Squashed from branch: $original_branch ($base_commit..$short_head)

Commits:
$commit_list

$coauthors"

    git branch -q -D _update_tmp 2>/dev/null || true
    git checkout -q -b _update_tmp HEAD
    git reset --soft "$base_commit"
    git commit -q -m "$commit_msg"
    local patch_commit=$(git rev-parse HEAD)

    # Create new branch from target tag and apply
    git checkout -q -b "$new_branch" "$target"
    if git cherry-pick "$patch_commit" 2>/dev/null; then
        git branch -q -D _update_tmp
        echo "✓ Updated to $target on branch: $new_branch"
        echo "  You are now on: $new_branch"
        echo ""
        echo "Next steps:"
        echo "  scripts/android.sh check"
        echo "  scripts/android.sh build --nocgo arm64"
        echo ""
        echo "To go back: git checkout $original_branch"
    else
        echo "✗ Conflicts on:"
        git diff --name-only --diff-filter=U 2>/dev/null | sed 's/^/  /'
        echo ""
        echo "Resolve with:"
        echo "  edit conflicted files"
        echo "  git add <file>"
        echo "  git cherry-pick --continue"
        echo ""
        echo "Or abort:"
        echo "  git cherry-pick --abort"
        echo "  git checkout $original_branch"
        echo "  git branch -D $new_branch _update_tmp"
    fi
}

# --- Main ---

case "${1:-}" in
    build)  shift; cmd_build "$@" ;;
    check)  shift; cmd_check "$@" ;;
    compat) shift; cmd_compat "$@" ;;
    update) shift; cmd_update "$@" ;;
    *)      echo "Usage: $0 {build|check|compat|update} [options]"; exit 1 ;;
esac
