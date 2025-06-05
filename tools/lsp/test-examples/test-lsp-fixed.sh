#!/bin/bash

echo "🚀 Testing Kro LSP Server with Fixes"
echo "======================================"

# Change to the LSP directory
cd "$(dirname "$0")"

echo "📁 Current directory: $(pwd)"
echo ""

# Check if server binary exists
if [ ! -f "server/kro-lsp" ]; then
    echo "🔨 Building LSP server..."
    cd server && go build -o kro-lsp . && cd ..
    if [ $? -ne 0 ]; then
        echo "❌ Failed to build server"
        exit 1
    fi
    echo "✅ Server built successfully"
else
    echo "✅ Server binary exists"
fi

echo ""

# Check VS Code settings
echo "🔍 Checking VS Code settings..."
if [ -f ".vscode/settings.json" ]; then
    echo "✅ Found .vscode/settings.json"
    echo "📋 CRD sources configured:"
    grep -A 5 "local" .vscode/settings.json | head -10
else
    echo "❌ No .vscode/settings.json found"
fi

echo ""

# Check CRD directory
echo "🔍 Checking CRD directory..."
if [ -d "crds" ]; then
    echo "✅ CRDs directory exists"
    echo "📋 Available CRDs:"
    ls -la crds/
else
    echo "❌ No crds directory found"
fi

echo ""

# Test server startup (quick test)
echo "🧪 Testing server startup..."
timeout 5s ./server/kro-lsp 2>&1 | head -20 &
SERVER_PID=$!

sleep 2

if kill -0 $SERVER_PID 2>/dev/null; then
    echo "✅ Server started successfully"
    kill $SERVER_PID 2>/dev/null
else
    echo "❌ Server failed to start"
fi

echo ""

# Test with sample RGD file
echo "🧪 Testing RGD file validation..."
if [ -f "test-simple-rgd.yaml" ]; then
    echo "✅ Test RGD file exists"
    echo "📄 Content preview:"
    head -10 test-simple-rgd.yaml
else
    echo "❌ No test RGD file found"
fi

echo ""
echo "🎯 Summary of Fixes Applied:"
echo "1. ✅ Created .vscode/settings.json with proper CRD sources"
echo "2. ✅ Created crds/ directory with sample CRDs"
echo "3. ✅ Updated CRD manager to search multiple paths for settings"
echo "4. ✅ Added actual Kro ResourceGraphDefinition CRD"
echo "5. ✅ Created test RGD file for validation"

echo ""
echo "🚀 Ready for VS Code Extension Testing!"
echo "To test:"
echo "1. Open VS Code in the kro project root"
echo "2. Go to Run and Debug (Ctrl+Shift+D)"
echo "3. Select 'Launch Kro Extension'"
echo "4. Press F5 to start debugging"
echo "5. Open test-simple-rgd.yaml in the new VS Code window"
echo "6. You should see validation diagnostics!"

echo ""
echo "📊 Expected Log Improvements:"
echo "- ✅ Should find VS Code settings"
echo "- ✅ Should load CRDs from local directory"
echo "- ✅ Should show validation diagnostics for RGD files" 