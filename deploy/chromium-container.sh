#!/bin/sh
# Chromium handles only escaped local HTML in this restricted non-root container.
set -eu
for landlord_chrome_arg in "$@"; do
  case "$landlord_chrome_arg" in
    --user-data-dir=*)
      # Crashpad also needs writable HOME/XDG paths, even with a custom profile.
      # Keep them inside the renderer's temporary directory for normal cleanup.
      landlord_chrome_runtime="${landlord_chrome_arg#--user-data-dir=}/runtime-home"
      mkdir -p "$landlord_chrome_runtime/config" "$landlord_chrome_runtime/cache"
      export HOME="$landlord_chrome_runtime"
      export XDG_CONFIG_HOME="$landlord_chrome_runtime/config"
      export XDG_CACHE_HOME="$landlord_chrome_runtime/cache"
      break
      ;;
  esac
done
exec /usr/bin/chromium --no-sandbox --disable-dev-shm-usage "$@"
