#!/bin/bash
# Build for macOS (Apple Silicon - ARM64)

echo "Building for macOS (arm64 - Apple Silicon)..."
wails build -platform darwin/arm64 -clean

# Ad-hoc sign to reduce Gatekeeper issues
if [ -d "build/bin/imagery-desktop.app" ]; then
    echo "Ad-hoc signing app bundle..."
    codesign -s - --force --deep build/bin/imagery-desktop.app
    xattr -cr build/bin/imagery-desktop.app
    echo "Signing complete!"
fi

echo "Build complete! Check build/bin/"
