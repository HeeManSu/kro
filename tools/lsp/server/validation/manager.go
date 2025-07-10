package validation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kro-run/kro/tools/lsp/server/parser"
	"github.com/tliron/commonlog"
)

type ValidationManager struct {
	logger        commonlog.Logger
	rgdValidator  *RGDValidator
	crdManager    *CRDManager
	workspaceRoot string
}

type ValidationResult struct {
	Errors []ValidationError
	Source string
}

type ValidationError struct {
	Message  string
	Range    parser.Range
	Severity string
	Source   string
}

func NewValidationManager(logger commonlog.Logger, workspaceRoot string) *ValidationManager {
	vm := &ValidationManager{
		logger:        logger,
		workspaceRoot: workspaceRoot,
	}

	// Initialize validators
	vm.rgdValidator = NewRGDValidator(logger)

	// Initialize CRD manager with default GitHub config
	crdConfig := CRDConfig{
		Enabled:     true,
		AutoRefresh: true,
		GitHubRepos: []GitHubConfig{
			{
				Owner:  "kubernetes",
				Repo:   "kubernetes",
				Path:   "api/openapi-spec/v3",
				Branch: "master",
				Token:  "",
			},
		},
	}
	vm.crdManager = NewCRDManager(logger, crdConfig)

	// Connect CRD manager to RGD validator
	vm.rgdValidator.SetCRDManager(vm.crdManager)

	// Load settings from VS Code
	vm.loadSettings()

	return vm
}

// loads validation settings from VS Code settings.json
func (vm *ValidationManager) loadSettings() {
	if vm.workspaceRoot == "TEMP_WORKSPACE_ROOT" {
		return
	}

	var settingsPaths []string
	var data []byte
	var err error
	var foundPath string

	// Note: Only for development purposes. Will be fixed in the future.
	// Start from workspace root and traverse up
	currentDir := vm.workspaceRoot

	for i := 0; i < 5; i++ {
		vm.logger.Debugf("Checking directory level %d: %s", i, currentDir)

		kroClientPath := filepath.Join(currentDir, "tools", "lsp", "client", ".vscode", "settings.json")
		settingsPaths = append(settingsPaths, kroClientPath)

		kroRootPath := filepath.Join(currentDir, ".vscode", "settings.json")
		settingsPaths = append(settingsPaths, kroRootPath)

		// Look for kro subdirectory (common case when workspace is kro-example but kro repo is nearby)
		kroSubdirClientPath := filepath.Join(currentDir, "kro", "tools", "lsp", "client", ".vscode", "settings.json")
		settingsPaths = append(settingsPaths, kroSubdirClientPath)

		kroSubdirRootPath := filepath.Join(currentDir, "kro", ".vscode", "settings.json")
		settingsPaths = append(settingsPaths, kroSubdirRootPath)

		// Go up one directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			vm.logger.Debugf("Reached filesystem root, stopping traversal")
			break
		}
		currentDir = parentDir
	}

	// Try each path until we find one that works
	for _, path := range settingsPaths {
		vm.logger.Debugf("Trying settings path: %s", path)
		data, err = os.ReadFile(path)
		if err == nil {
			foundPath = path
			vm.logger.Infof("Loaded settings from: %s", foundPath)
			break
		} else {
			vm.logger.Debugf("Not found: %v", err)
		}
	}

	if err != nil {
		vm.logger.Debugf("No VS Code settings found in any location, using defaults")
		vm.logger.Debugf("Tried paths: %v", settingsPaths)
		return
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		vm.logger.Warningf("Failed to parse VS Code settings: %v", err)
		return
	}

	if crdSettings, exists := settings["kro.crd"]; exists {
		var crdConfig CRDConfig
		if configBytes, err := json.Marshal(crdSettings); err == nil {
			if err := json.Unmarshal(configBytes, &crdConfig); err != nil {
				vm.logger.Warningf("Failed to parse CRD config: %v", err)
			} else {
				vm.updateCRDManagerSources(crdConfig)
			}
		}
	}

	vm.logger.Infof("Loaded validation settings from VS Code")

	// Load CRDs
	vm.logger.Infof("CRD Manager enabled: %v", vm.crdManager.IsEnabled())
	if vm.crdManager.IsEnabled() {
		ctx := context.Background()
		if err := vm.crdManager.LoadCRDs(ctx); err != nil {
			vm.logger.Warningf("Failed to load CRDs: %v", err)
		} else {
			vm.logger.Infof("CRD loading completed successfully")
		}
	} else {
		vm.logger.Warningf("CRD validation is disabled")
	}
}

// updates the sources of the existing CRD manager
func (vm *ValidationManager) updateCRDManagerSources(config CRDConfig) {
	vm.crdManager.updateConfig(config)
}

func (vm *ValidationManager) ValidateDocument(ctx context.Context, uri string, parsed *parser.ParsedYAML) *ValidationResult {
	result := &ValidationResult{
		Source: uri,
	}

	// structural and syntax validation
	rgdErrors := vm.rgdValidator.ValidateRGD(parsed)
	result.Errors = append(result.Errors, rgdErrors...)
	return result
}

// returns basic information about CRD validation status
// func (vm *ValidationManager) GetCRDInfo() map[string]interface{} {
// 	if vm.crdManager == nil {
// 		return map[string]interface{}{
// 			"enabled": false,
// 			"reason":  "CRD manager not initialized",
// 		}
// 	}

// 	if !vm.crdManager.IsEnabled() {
// 		return map[string]interface{}{
// 			"enabled": false,
// 			"reason":  "CRD validation disabled",
// 		}
// 	}

// 	return map[string]interface{}{
// 		"enabled": true,
// 		"status":  "CRD validation active",
// 	}
// }
