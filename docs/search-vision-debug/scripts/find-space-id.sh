#!/usr/bin/env bash
# find-space-id.sh — prints the storage ID and space ID for all users in posixfs storage.
#
# Run on the Odroid (host).
#
# Usage:
#   ./find-space-id.sh

USER_STORAGE="$HOME/opencloud/opencloud-data/storage/users/users"

for user_dir in "$USER_STORAGE"/*/; do
  user_id=$(basename "$user_dir")
  space_id=$(getfattr -n user.oc.space.id "$user_dir" 2>/dev/null \
    | grep 'user.oc.space.id=' \
    | sed 's/.*="\(.*\)"/\1/')

  if [[ -n "$space_id" ]]; then
    echo "user: $user_id"
    echo "  space.id xattr: $space_id"
  else
    echo "user: $user_id  (no space.id xattr found)"
  fi
done
