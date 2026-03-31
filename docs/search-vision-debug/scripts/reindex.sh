#!/usr/bin/env bash
# reindex.sh — wipe bleve and run a full fresh reindex for the personal space.
#
# Run on the Odroid (host), not inside the container.
#
# Usage:
#   ./reindex.sh
#   ./reindex.sh --touch-all   # also touch all files to force re-extraction
#
# Give the storage and space IDs as arguments and fail if the arguments are short
#


set -euo pipefail

# Default values
STORAGE_ID="5fa3d061-53cf-47c1-85b5-ded19ae3b6ed"
SPACE_ID="cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe"
BLEVE_DIR="$HOME/opencloud/opencloud-data/search/bleve"
CONTAINER="opencloud"

# Parse arguments
while [[ "$#" -gt 0 ]]; do
    case $1 in
        -storage_id) STORAGE_ID="$2"; shift ;;
        -space_id)   SPACE_ID="$2";   shift ;;
        -bleve_dir)  BLEVE_DIR="$2";  shift ;;
        -container)  CONTAINER="$2";  shift ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
    shift
done

# Output results to verify
echo "Options used:"
echo "STORAGE_ID: $STORAGE_ID"
echo "SPACE_ID:   $SPACE_ID"
echo "BLEVE_DIR:  $BLEVE_DIR"
echo "CONTAINER:  $CONTAINER"

TOUCH_ALL=false
if [[ "${1:-}" == "--touch-all" ]]; then
  TOUCH_ALL=true
fi

echo "Stopping the container..."
docker stop "$CONTAINER"

echo "Wipe bleve index at $BLEVE_DIR"
rm -rf "$BLEVE_DIR"

if $TOUCH_ALL; then
  USER_STORAGE="$HOME/opencloud/opencloud-data/storage/users/users"
  echo "Touch all files to force re-extract"
  find "$USER_STORAGE" -type f ! -name '.*' -exec touch {} \;
fi

echo "Start the container"
docker start "$CONTAINER"
echo "Waiting for the services "
sleep 10

echo "Running reindex for space ${STORAGE_ID}\$${SPACE_ID} ..."
docker exec "$CONTAINER" opencloud search index \
  --space "${STORAGE_ID}\$${SPACE_ID}"

echo "Done."
