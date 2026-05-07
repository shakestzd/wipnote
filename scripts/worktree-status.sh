#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/worktree-status.sh [--base-dir DIR]
# Shows status of all parallel worktrees

BASE_DIR="worktrees"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --base-dir)
            BASE_DIR="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--base-dir DIR]"
            exit 1
            ;;
    esac
done

echo "=== Parallel Worktree Status ==="
echo ""

if [ ! -d "$BASE_DIR" ]; then
    echo "No worktrees found. Run: wipnote yolo --track <trk-id>"
    exit 0
fi

# Count total worktrees
TOTAL=$(ls -d "$BASE_DIR"/*/ 2>/dev/null | wc -l | tr -d ' ')

if [ "$TOTAL" -eq 0 ]; then
    echo "No active worktrees."
    exit 0
fi

printf "%-25s %-30s %-10s %-40s\n" "TASK" "BRANCH" "COMMITS" "LAST COMMIT"
printf "%-25s %-30s %-10s %-40s\n" "----" "------" "-------" "-----------"

for wt in "$BASE_DIR"/*/; do
    [ -d "$wt" ] || continue
    name=$(basename "$wt")
    branch=$(cd "$wt" && git branch --show-current 2>/dev/null || echo "detached")
    commits=$(cd "$wt" && git log --oneline origin/main..HEAD 2>/dev/null | wc -l | tr -d ' ')
    last=$(cd "$wt" && git log --oneline -1 2>/dev/null | cut -c1-40 || echo "no commits")

    printf "%-25s %-30s %-10s %-40s\n" "$name" "$branch" "$commits" "$last"
done

echo ""
echo "Total worktrees: $TOTAL"
