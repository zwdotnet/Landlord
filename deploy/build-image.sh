#!/bin/sh
# Run on a dedicated Linux Docker host, not the small production VPS.
set -eu
cd "$(dirname "$0")/.."
landlord_available_kb=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
if [ "${landlord_available_kb:-0}" -lt 3145728 ]; then
  echo 'Build refused: at least 3 GiB available RAM is required. Use a separate Linux builder.' >&2
  exit 1
fi
test -f web/dist/index.html || { echo 'Build the production frontend first.' >&2; exit 1; }
docker buildx version >/dev/null
landlord_builder=${LANDLORD_BUILDER_NAME:-landlord-candidate-$(date +%s)-$$}
landlord_created=0
landlord_image=${1:-landlord:0.10.0-20261111-final}
landlord_buildkit_image=${LANDLORD_BUILDKIT_IMAGE:-moby/buildkit:buildx-stable-1}
landlord_buildkit_config=${LANDLORD_BUILDKIT_CONFIG:-deploy/buildkitd.toml}
cleanup() {
  if [ "$landlord_created" = 1 ]; then
    if [ "${LANDLORD_KEEP_BUILD_CACHE:-0}" = 1 ]; then
      docker buildx rm --keep-state "$landlord_builder" >/dev/null 2>&1 || true
    else
      docker buildx rm "$landlord_builder" >/dev/null 2>&1 || true
    fi
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker buildx create --name "$landlord_builder" --driver docker-container \
  --driver-opt "image=$landlord_buildkit_image" \
  --driver-opt memory=1536m,memory-swap=1536m,cpu-period=100000,cpu-quota=100000,restart-policy=no \
  --buildkitd-config "$landlord_buildkit_config" >/dev/null
landlord_created=1
docker buildx build --builder "$landlord_builder" --load --progress=plain \
  --build-arg "APP_VERSION=$(tr -d '\r\n' < VERSION)" \
  --build-arg "GOPROXY=${LANDLORD_GOPROXY:-https://proxy.golang.org,direct}" \
  --build-arg "DEBIAN_MIRROR=${LANDLORD_DEBIAN_MIRROR:-http://deb.debian.org/debian}" \
  --build-arg "DEBIAN_SECURITY_MIRROR=${LANDLORD_DEBIAN_SECURITY_MIRROR:-http://deb.debian.org/debian-security}" \
  -f deploy/Dockerfile -t "$landlord_image" .
