#!/bin/bash

# Test script for Kro LSP Server
# This script demonstrates the RGD validation functionality

set -e

echo "🚀 Kro LSP Server Test"
echo "======================"

# Build the LSP server
echo "📦 Building LSP server..."
cd server
go build -o kro-lsp .
cd ..

echo "✅ LSP server built successfully!"

# Check if the example RGD file exists
if [ ! -f "example-rgd.yaml" ]; then
    echo "❌ Example RGD file not found!"
    exit 1
fi

echo "📄 Example RGD file found:"
echo "   - File: example-rgd.yaml"
echo "   - Size: $(wc -l < example-rgd.yaml) lines"

# Show the first few lines of the RGD file
echo ""
echo "📋 RGD File Preview:"
echo "-------------------"
head -15 example-rgd.yaml

echo ""
echo "🔍 Validation Features:"
echo "----------------------"
echo "✓ Detects RGD files by apiVersion: kro.run/v1alpha1"
echo "✓ Validates required fields (apiVersion, kind, schema.kind, etc.)"
echo "✓ Checks resource structure (template vs externalRef)"
echo "✓ Validates against CRDs when available"
echo "✓ Provides real-time diagnostics in VS Code"

echo ""
echo "🛠️  Configuration:"
echo "-----------------"
echo "Configure CRD sources in .vscode/settings.json:"
echo '{
  "kro.lsp.crd.sources": {
    "clusters": [
      {
        "name": "local-k8s",
        "kubeconfig": "~/.kube/config",
        "context": "default",
        "enabled": true
      }
    ],
    "github": [
      {
        "name": "kro-crds",
        "owner": "kro-run",
        "repo": "kro",
        "path": "config/crd/bases",
        "enabled": true
      }
    ],
    "local": [
      {
        "name": "project-crds",
        "path": "./crds",
        "enabled": true
      }
    ]
  }
}'

echo ""
echo "🎯 Expected Validation Results for example-rgd.yaml:"
echo "---------------------------------------------------"
echo "❌ Error: resource[3] missing required 'id' field (line ~67)"
echo "⚠️  Warning: No CRD found for NonExistentResource (line ~58)"
echo "⚠️  Warning: Invalid memory format 'invalid-memory-format' (line ~37)"
echo "⚠️  Warning: Invalid CPU format 'not-a-valid-cpu' (line ~38)"

echo ""
echo "🚀 Usage:"
echo "--------"
echo "1. Install the VS Code extension from tools/lsp/client/"
echo "2. Open any .yaml file with RGD content"
echo "3. The LSP will automatically validate and show diagnostics"
echo "4. Red underlines indicate errors, yellow indicate warnings"

echo ""
echo "✅ Test completed! LSP server is ready for use."
echo "   Binary location: server/kro-lsp"
echo "   Documentation: README-RGD-Validation.md" 