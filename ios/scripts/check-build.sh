#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
if ! command -v xcodebuild >/dev/null; then
  echo 'Native builds require macOS with Xcode 26 or later.' >&2
  exit 1
fi
xcodebuild -version
lester_derived="$(mktemp -d "${TMPDIR:-/tmp}/lester-ios-build.XXXXXX")"
trap 'rm -rf "$lester_derived"' EXIT
for platform in 'generic/platform=iOS Simulator' 'generic/platform=iOS'; do
  xcodebuild -project Lester.xcodeproj -scheme Lester -configuration Release \
    -destination "$platform" -derivedDataPath "$lester_derived" \
    CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
done
xcodebuild -project Lester.xcodeproj -scheme Lester -configuration Debug \
  -destination 'generic/platform=iOS Simulator' -derivedDataPath "$lester_derived" \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
