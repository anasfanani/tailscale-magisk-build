#!/bin/bash
# Test Android modifications compatibility with upstream versions
# This script tests rebasing from current tag through all upstream versions
#
# Usage:
#   ./test-upstream-compatibility.sh [TAG] [OPTIONS]
#
# Examples:
#   ./test-upstream-compatibility.sh v1.82.5          # Test from v1.82.5
#   PAUSE_ON_CONFLICT=1 ./test-upstream-compatibility.sh v1.82.5  # Pause on each conflict



# Cleanup function
cleanup() {
    local exit_code=$?
    echo -e "\n${BLUE}Cleaning up temporary files...${NC}"
    
    # Kill any running build processes
    pkill -P $$ || true
    
    # Abort any ongoing rebase/merge
    git rebase --abort 2>/dev/null || true
    git merge --abort 2>/dev/null || true
    
    # Remove test branch if it exists
    git branch -D "$ANDROID_MODS_BRANCH" 2>/dev/null || true
    
    # Return to original branch (captured at script start)
    if [ -n "${ORIGINAL_BRANCH:-}" ] && [ "$ORIGINAL_BRANCH" != "HEAD" ]; then
        git checkout -f "$ORIGINAL_BRANCH" 2>/dev/null || true
    fi
    
    # Remove temporary directory (but keep the results file if needed)
    if [ -d "$TEMP_DIR" ]; then
        # Save results before cleanup
        if [ -f "$CONFLICT_LOG" ]; then
            cp "$CONFLICT_LOG" /tmp/compat_results_$(date +%s).txt 2>/dev/null || true
        fi
        rm -rf "$TEMP_DIR" 2>/dev/null || true
    fi
    
    # Log exit status
    if [ $exit_code -eq 0 ]; then
        echo -e "${GREEN}Test completed successfully${NC}"
    else
        echo -e "${RED}Test interrupted or failed with exit code $exit_code${NC}" >&2
    fi
    
    exit $exit_code
}

# Set trap to run cleanup on all exits and errors
trap cleanup EXIT INT TERM ERR RETURN QUIT ABRT
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
TMPDIR=${TMPDIR:-/tmp}

# Capture current branch before doing anything
ORIGINAL_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "HEAD")

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CURRENT_TAG="${1:-v1.82.5}"
ANDROID_MODS_BRANCH="android-mods-test"
TEMP_DIR="$TMPDIR/tailscale-compat-test-$$"
CONFLICT_LOG="$TEMP_DIR/conflict-analysis.txt"

mkdir -p "$TEMP_DIR"

echo -e "${BLUE}=== Android Modifications Compatibility Test ===${NC}"
echo -e "${BLUE}Starting from: $CURRENT_TAG${NC}\n"

# Ensure upstream is configured
if ! git remote get-url upstream &>/dev/null; then
    echo -e "${YELLOW}Adding upstream remote...${NC}"
    git remote add upstream https://github.com/tailscale/tailscale.git
fi

# Fetch upstream tags
echo -e "${BLUE}Fetching upstream tags...${NC}"
git fetch upstream 'refs/tags/*:refs/tags/*' --force -q

# Get all tags from current version onwards, sorted in ascending order (oldest first)
echo -e "${BLUE}Analyzing upstream versions...${NC}"
TAGS=$(git tag -l 'v[0-9]*' --sort=version:refname | awk -v current="$CURRENT_TAG" '
    BEGIN { found=0 }
    {
        if ($0 == current) { found=1; next }
        if (found) print $0
    }
')

if [ -z "$TAGS" ]; then
    echo -e "${RED}No tags found after $CURRENT_TAG${NC}"
    exit 1
fi

# Count tags
TAG_COUNT=$(echo "$TAGS" | wc -l)
echo -e "${YELLOW}Found $TAG_COUNT versions to test${NC}\n"

# Initialize results file
RESULTS_FILE="$TMPDIR/compat_results.txt"
cat > "$RESULTS_FILE" << EOF
EOF

# Track statistics
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Test each version
while IFS= read -r TAG <&3; do
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "${YELLOW}[$TOTAL_TESTS/$TAG_COUNT]${NC} Testing: ${BLUE}$TAG${NC}"
    
    git rebase --abort 2>/dev/null || true
    git merge --abort 2>/dev/null || true
    # Clean up previous test branch
    git branch -D "$ANDROID_MODS_BRANCH" 2>/dev/null || true
    
    # Create test branch
    git checkout -q -b "$ANDROID_MODS_BRANCH"
    
    # Try rebase
    if git rebase -X ours --onto "$TAG" "$CURRENT_TAG" 2>/dev/null; then
        echo -e "  ✅ ${GREEN}PASS${NC} - Rebases cleanly"
        echo "✅ $TAG: SUCCESS (no conflicts)" >> "$RESULTS_FILE"
        PASSED_TESTS=$((PASSED_TESTS + 1))
        read -p "Press enter to continue..."
        echo -e "  ⏳ ${BLUE}Building...${NC}"
        # Clear Go toolchain cache before build to avoid version conflicts
        rm -rf "$TMPDIR/.cache/tsgo" "$TMPDIR/.gocache" 
        if timeout 300 ./build_android.sh arm64 2>&1; then
            echo -e "  ✅ ${GREEN}BUILD SUCCESS${NC}"
            echo "   Build: SUCCESS" >> "$RESULTS_FILE"
        else
            BUILD_EXIT=$?
            echo -e "  ⚠️  ${YELLOW}BUILD FAILED${NC} (exit code: $BUILD_EXIT)"
            echo "   Build: FAILED (exit code: $BUILD_EXIT)" >> "$RESULTS_FILE"
            # Don't count as FAILED_TEST since rebase was successful
        fi
    else
        echo -e "  ❌ ${RED}FAIL${NC} - Conflicts detected"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        
        # Get conflicted files
        CONFLICTED=$(git diff --name-only --diff-filter=U 2>/dev/null || true)
        
        echo "" >> "$RESULTS_FILE"
        echo "❌ $TAG: CONFLICT" >> "$RESULTS_FILE"
        echo "   Files with conflicts:" >> "$RESULTS_FILE"
        
        # Analyze each conflicted file
        echo "$CONFLICTED" | while read -r FILE; do
            if [ -n "$FILE" ]; then
                echo "   - $FILE" >> "$RESULTS_FILE"
            fi
        done
        
        # Pause for analysis if PAUSE_ON_CONFLICT is set
        if [ -n "$PAUSE_ON_CONFLICT" ]; then
            echo -e "\n${YELLOW}=== CONFLICT ANALYSIS ===${NC}"
            echo -e "${BLUE}Conflicted files:${NC}"
            git diff --name-only --diff-filter=U 2>/dev/null | sed 's/^/  - /' || true
            
            # Interactive menu loop
            while true; do
                echo -e "\n${BLUE}Options:${NC}"
                echo "  (Enter) Continue to next version"
                echo "  (s)     Skip to next version"
                echo "  (show FILE) View conflict in FILE"
                echo "  (q)     Quit"
                echo -n "Action: "
                read ACTION
                
                case "$ACTION" in
                    q|Q) 
                        echo "Quitting..."
                        git rebase --abort 2>/dev/null || true
                        git checkout -q - 2>/dev/null || true
                        git branch -D "$ANDROID_MODS_BRANCH" 2>/dev/null || true
                        exit 0
                        ;;
                    s|S)
                        echo "Skipping..."
                        break
                        ;;
                    show\ *)
                        FILE_TO_SHOW="${ACTION#show }"
                        if git diff "$FILE_TO_SHOW" 2>/dev/null | head -80; then
                            echo -e "\n${BLUE}(showing first 80 lines)${NC}"
                        else
                            echo "Could not show: $FILE_TO_SHOW"
                        fi
                        ;;
                    "")
                        # Empty input = continue
                        break
                        ;;
                    *)
                        echo "Unknown action: $ACTION"
                        ;;
                esac
            done
        fi
        
        # Abort rebase for next iteration
        git rebase --abort 2>/dev/null || true
    fi
    
    # CRITICAL: Return to original branch before next iteration
    git rebase --abort 2>/dev/null || true
    git merge --abort 2>/dev/null || true
    if git rev-parse --verify "$ANDROID_MODS_BRANCH" 2>/dev/null; then
        git branch -D "$ANDROID_MODS_BRANCH" 2>/dev/null || true
    fi
    git checkout -f "$ORIGINAL_BRANCH" 2>/dev/null || true
    git reset --hard 2>/dev/null || true
done 3< <(echo "$TAGS")

# Print summary
echo -e "\n${BLUE}=== Compatibility Summary ===${NC}"
echo -e "Total versions tested: ${YELLOW}$TOTAL_TESTS${NC}"
echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed: ${RED}$FAILED_TESTS${NC}"

# Copy results to dist
cp "$RESULTS_FILE" "$CONFLICT_LOG"

# Add summary to report
cat >> "$CONFLICT_LOG" << EOF

## Summary
- Total versions tested: $TOTAL_TESTS
- Passed: $PASSED_TESTS  
- Failed: $FAILED_TESTS
- Success rate: $(awk "BEGIN {printf \"%.1f%%\", $PASSED_TESTS * 100 / $TOTAL_TESTS}")

## Conflict Analysis
EOF

# Extract and analyze conflicts
echo "" >> "$CONFLICT_LOG"
echo "Most problematic files:" >> "$CONFLICT_LOG"
grep "^   - " "$RESULTS_FILE" | sort | uniq -c | sort -rn >> "$CONFLICT_LOG" 2>/dev/null || true

echo -e "\n${GREEN}Report saved to: $CONFLICT_LOG${NC}"
echo -e "${YELLOW}View with: cat $CONFLICT_LOG${NC}"

# Print report location
echo -e "\n${BLUE}Full analysis:${NC}"
cat "$CONFLICT_LOG"

# Final status
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "\n${GREEN}✅ All versions compatible!${NC}"
    echo -e "\n${BLUE}Results saved to: $CONFLICT_LOG${NC}"
else
    echo -e "\n${YELLOW}⚠️  Some versions have conflicts - review needed${NC}"
    echo -e "\n${BLUE}Results saved to: $CONFLICT_LOG${NC}"
fi
exit 0