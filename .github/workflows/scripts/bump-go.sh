#!/usr/bin/env bash
#
# bump-go.sh — Update go.mod `go` directive and toolchain to latest stable Go release.
#
# Usage:
#   ./bump-go.sh [--apply|-a] <path/to/go.mod>
#
# By default the script runs in *dry‑run* mode: it creates a local branch,
# commits the version bump, shows the exact patch, **checks for an existing PR**
# with the same title, and exits. Nothing is pushed. The temporary branch is
# deleted automatically on exit, so your working tree stays clean. Pass
# --apply (or -a) to push the branch and open a new PR *only if one doesn’t
# already exist*.
# -----------------------------------------------------------------------------
set -euo pipefail

usage() {
  echo "Usage: $0 [--apply|-a] <path/to/go.mod>" >&2
  exit 1
}

# ---- Argument parsing -------------------------------------------------------
APPLY=0
GO_MOD=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply|-a) APPLY=1 ;;
    -h|--help)  usage ;;
    *)          [[ -z "$GO_MOD" ]] && GO_MOD="$1" || usage ;;
  esac
  shift
done

[[ -z "$GO_MOD" ]] && usage
[[ -f "$GO_MOD" ]] || { echo "Error: '$GO_MOD' not found" >&2; exit 1; }

# ---- Discover latest stable Go release --------------------------------------
echo "Fetching latest stable Go version…"
LATEST_JSON=$(curl -fsSL https://go.dev/dl/?mode=json | jq -c '[.[] | select(.stable==true)][0]')
FULL_VERSION=$(jq -r '.version' <<< "$LATEST_JSON")        # e.g. go1.23.4
TOOLCHAIN_VERSION="${FULL_VERSION#go}"                     # e.g. 1.23.4

# The go directive can be either X.Y.0 (minor version) or X.Y.Z (patch version)
# We accept both forms as "latest" if they match the toolchain's major.minor
LATEST_MAJOR_MINOR="$(cut -d. -f1-2 <<< "$TOOLCHAIN_VERSION")"

echo "  → latest toolchain : $TOOLCHAIN_VERSION"

# ---- Prepare Git branch ---------------------------------------------------
CURRENT_GO_DIRECTIVE=$(grep -E '^go ' "$GO_MOD" | cut -d ' ' -f2)
CURRENT_TOOLCHAIN_DIRECTIVE=$(grep -E '^toolchain ' "$GO_MOD" | cut -d ' ' -f2 || true)

CURRENT_MAJOR_MINOR="$(cut -d. -f1-2 <<< "$CURRENT_GO_DIRECTIVE")"

BRANCH="bump-go-$TOOLCHAIN_VERSION"
BRANCH_CREATED=0

# Set up cleanup trap early (before any potential exits)
cleanup() {
  if [[ $BRANCH_CREATED -eq 1 ]]; then
    git checkout - >/dev/null 2>&1 || true
    git branch -D "$BRANCH" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# Check if we're already up to date
# Note: toolchain directive may be missing when go directive >= latest toolchain.
# This is expected behavior - `go mod tidy` removes the toolchain line when
# the minimum Go version matches or exceeds the latest toolchain, as it's redundant.
if [[ "$CURRENT_MAJOR_MINOR" = "$LATEST_MAJOR_MINOR" ]]; then
  # Current go directive is at the same major.minor as latest
  if [[ -z "$CURRENT_TOOLCHAIN_DIRECTIVE" ]]; then
    # No toolchain directive present - this is expected when go version >= latest
    echo "Already on latest Go version: $CURRENT_GO_DIRECTIVE (latest toolchain: $TOOLCHAIN_VERSION)"
    echo "  → Note: No toolchain directive (expected when go version matches latest toolchain)"
    exit 0
  elif [[ "$CURRENT_TOOLCHAIN_DIRECTIVE" = "go$TOOLCHAIN_VERSION" ]]; then
    echo "Already on latest Go version: $CURRENT_GO_DIRECTIVE (toolchain: $CURRENT_TOOLCHAIN_DIRECTIVE)"
    exit 0
  fi
  # Current go directive is latest but toolchain is outdated - continue to update toolchain
fi

echo "Creating branch $BRANCH"
git switch -c "$BRANCH" >/dev/null 2>&1
BRANCH_CREATED=1

# ---- Patch go.mod -----------------------------------------------------------
# Only update go directive if we're not already at the latest major.minor version
if [[ "$CURRENT_MAJOR_MINOR" != "$LATEST_MAJOR_MINOR" ]]; then
  # Bump to the latest major.minor.0 (preserves the convention of X.Y.0 for go directive)
  NEW_GO_DIRECTIVE="$LATEST_MAJOR_MINOR.0"
  sed -Ei.bak "s/^go [0-9]+\.[0-9]+.*$/go $NEW_GO_DIRECTIVE/" "$GO_MOD"
  echo "  • go directive $CURRENT_GO_DIRECTIVE → $NEW_GO_DIRECTIVE"
  # After updating, the current go directive is now the new one for toolchain logic
  CURRENT_GO_DIRECTIVE="$NEW_GO_DIRECTIVE"
fi

# Handle toolchain directive - may need to add, update, or skip
if [[ -z "$CURRENT_TOOLCHAIN_DIRECTIVE" ]]; then
  # No toolchain directive exists
  CURRENT_MAJOR_MINOR="$(cut -d. -f1-2 <<< "$CURRENT_GO_DIRECTIVE")"
  if [[ "$CURRENT_MAJOR_MINOR" = "$LATEST_MAJOR_MINOR" ]]; then
    # go directive is at latest major.minor - toolchain line is redundant
    echo "  • toolchain directive not needed (go version matches latest toolchain)"
  else
    # go directive is older than latest toolchain - add toolchain directive after go line
    sed -Ei.bak "/^go [0-9]+\.[0-9]+/a\\
toolchain go$TOOLCHAIN_VERSION" "$GO_MOD"
    echo "  • toolchain directive added: go$TOOLCHAIN_VERSION"
  fi
elif [[ "$CURRENT_TOOLCHAIN_DIRECTIVE" != "go$TOOLCHAIN_VERSION" ]]; then
  # Toolchain directive exists but needs updating
  sed -Ei.bak "s/^toolchain go[0-9]+\.[0-9]+\.[0-9]+.*$/toolchain go$TOOLCHAIN_VERSION/" "$GO_MOD"
  echo "  • toolchain $CURRENT_TOOLCHAIN_DIRECTIVE → go$TOOLCHAIN_VERSION"
fi

rm -f "$GO_MOD.bak"

git add "$GO_MOD"

# ---- Commit -----------------------------------------------------------------
COMMIT_MSG="Bump Go to $TOOLCHAIN_VERSION"
git commit -m "$COMMIT_MSG" >/dev/null
COMMIT_HASH=$(git rev-parse --short HEAD)

PR_TITLE="$COMMIT_MSG"

# ---- Check for existing PR --------------------------------------------------
existing_pr=$(gh search prs --repo cli/cli --match title "$PR_TITLE" --json title --jq "map(select(.title == \"$PR_TITLE\") | .title) | length > 0")

if [[ "$existing_pr" == "true" ]]; then
  echo "Found an existing open PR titled '$PR_TITLE'. Skipping push/PR creation."
  if [[ $APPLY -eq 0 ]]; then
    echo -e "\n=== DRY‑RUN DIFF (commit $COMMIT_HASH):\n"
    git --no-pager show --color "$COMMIT_HASH"
  fi
  exit 0
fi

# ---- Dry‑run handling -------------------------------------------------------
if [[ $APPLY -eq 0 ]]; then
  echo -e "\n=== DRY‑RUN DIFF (commit $COMMIT_HASH):\n"
  git --no-pager show --color "$COMMIT_HASH"
  echo -e "\nIf --apply were provided, script would continue with:\n  git push -u origin $BRANCH\n  gh pr create --title \"$PR_TITLE\" --body <body>\n"
  exit 0
fi

# ---- Push & PR --------------------------------------------------------------
# Get the actual go directive from the updated go.mod
FINAL_GO_DIRECTIVE=$(grep -E '^go ' "$GO_MOD" | cut -d ' ' -f2)

PR_BODY=$(cat <<EOF
This PR updates Go to the latest stable release.

* **go directive:** \`$FINAL_GO_DIRECTIVE\`
* **toolchain:** \`$TOOLCHAIN_VERSION\`
EOF
)

git push -u origin "$BRANCH"

gh pr create --title "$PR_TITLE" --body "$PR_BODY" --fill

echo "Done!"
