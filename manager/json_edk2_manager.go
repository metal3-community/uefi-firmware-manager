package manager

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmcpi/uefi-firmware-manager/efi"
	"github.com/bmcpi/uefi-firmware-manager/types"
	"github.com/go-logr/logr"
)

// JsonEDK2Manager manages UEFI firmware using JSON files organized by MAC address.
type JsonEDK2Manager struct {
	dataDir    string           // Base directory containing MAC subdirectories
	currentMAC net.HardwareAddr // Currently selected MAC address
	variables  efi.EfiVarList   // Currently loaded variables
	logger     logr.Logger
	modified   bool // Track if variables have been modified
}

// NewJsonEDK2Manager creates a new JSON-based EDK2 manager.
func NewJsonEDK2Manager(dataDir string, logger logr.Logger) (*JsonEDK2Manager, error) {
	manager := &JsonEDK2Manager{
		dataDir:   dataDir,
		variables: make(efi.EfiVarList),
		logger:    logger,
	}

	// Verify data directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	return manager, nil
}

// LoadMAC loads variables for a specific MAC address.
func (j *JsonEDK2Manager) LoadMAC(mac net.HardwareAddr) error {
	j.logger.Info("Loading variables for MAC", "mac", mac.String())

	macDir := j.macDirName(mac)
	jsonPath := filepath.Join(j.dataDir, macDir, "fw-vars.json")

	variables, err := j.loadVariablesFromJSON(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to load variables for MAC %s: %w", mac.String(), err)
	}

	j.currentMAC = mac
	j.variables = variables
	j.modified = false

	// Validate that the loaded MAC matches the directory structure
	if err := j.validateMACConsistency(); err != nil {
		j.logger.Info("MAC validation warning", "error", err)
	}

	return nil
}

// ListAvailableMACs returns all MAC addresses that have configuration directories.
func (j *JsonEDK2Manager) ListAvailableMACs() ([]net.HardwareAddr, error) {
	entries, err := os.ReadDir(j.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var macs []net.HardwareAddr
	for _, entry := range entries {
		if entry.IsDir() {
			mac, err := j.macFromDirName(entry.Name())
			if err != nil {
				j.logger.Info("Skipping invalid MAC directory", "dir", entry.Name(), "error", err)
				continue
			}

			// Verify fw-vars.json exists
			jsonPath := filepath.Join(j.dataDir, entry.Name(), "fw-vars.json")
			if _, err := os.Stat(jsonPath); err == nil {
				macs = append(macs, mac)
			}
		}
	}

	return macs, nil
}

// GetCurrentMAC returns the currently loaded MAC address.
func (j *JsonEDK2Manager) GetCurrentMAC() net.HardwareAddr {
	return j.currentMAC
}

// macDirName converts a MAC address to directory name format (colons to hyphens).
func (j *JsonEDK2Manager) macDirName(mac net.HardwareAddr) string {
	return strings.ReplaceAll(mac.String(), ":", "-")
}

// macFromDirName converts a directory name to MAC address (hyphens to colons).
func (j *JsonEDK2Manager) macFromDirName(dirName string) (net.HardwareAddr, error) {
	macStr := strings.ReplaceAll(dirName, "-", ":")
	return net.ParseMAC(macStr)
}

// loadVariablesFromJSON loads EFI variables from a JSON file.
func (j *JsonEDK2Manager) loadVariablesFromJSON(jsonPath string) (efi.EfiVarList, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON file: %w", err)
	}

	var variables efi.EfiVarList
	if err := json.Unmarshal(data, &variables); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	j.logger.Info("Loaded variables from JSON", "path", jsonPath, "count", len(variables))
	return variables, nil
}

// saveVariablesToJSON saves EFI variables to a JSON file.
func (j *JsonEDK2Manager) saveVariablesToJSON(jsonPath string, variables efi.EfiVarList) error {
	data, err := json.MarshalIndent(variables, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write JSON file: %w", err)
	}

	j.logger.Info("Saved variables to JSON", "path", jsonPath, "count", len(variables))
	return nil
}

// validateMACConsistency checks if the loaded ClientId variable matches the current MAC.
func (j *JsonEDK2Manager) validateMACConsistency() error {
	if j.currentMAC == nil {
		return fmt.Errorf("no MAC address loaded")
	}

	clientIdVar, exists := j.variables["ClientId"]
	if !exists {
		return fmt.Errorf("ClientId variable not found")
	}

	duid, err := efi.NewDhcp6Duid(clientIdVar.Data)
	if err != nil {
		return fmt.Errorf("failed to parse ClientId DUID: %w", err)
	}

	extractedMAC := duid.GetMacAddress()
	if extractedMAC == nil {
		// Not all DUID types contain MAC addresses, this is OK
		return nil
	}

	if !slices.Equal(j.currentMAC, extractedMAC) {
		return fmt.Errorf("MAC mismatch: directory=%s, ClientId=%s",
			j.currentMAC.String(), extractedMAC.String())
	}

	return nil
}

// FirmwareManager interface implementation

// GetMacAddress returns the currently loaded MAC address.
func (j *JsonEDK2Manager) GetMacAddress() (net.HardwareAddr, error) {
	if j.currentMAC == nil {
		return nil, fmt.Errorf("no MAC address loaded")
	}
	return j.currentMAC, nil
}

// SetMacAddress sets the MAC address (loads different configuration).
func (j *JsonEDK2Manager) SetMacAddress(mac net.HardwareAddr) error {
	return j.LoadMAC(mac)
}

// GetVariable retrieves a specific EFI variable.
func (j *JsonEDK2Manager) GetVariable(name string) (*efi.EfiVar, error) {
	if j.variables == nil {
		return nil, fmt.Errorf("no variables loaded")
	}

	variable, exists := j.variables[name]
	if !exists {
		return nil, fmt.Errorf("variable %s not found", name)
	}

	return variable, nil
}

// SetVariable sets a specific EFI variable.
func (j *JsonEDK2Manager) SetVariable(name string, value *efi.EfiVar) error {
	if j.variables == nil {
		return fmt.Errorf("no variables loaded")
	}

	j.variables[name] = value
	j.modified = true

	j.logger.Info("Variable updated", "name", name)
	return nil
}

// ListVariables returns all loaded variables.
func (j *JsonEDK2Manager) ListVariables() (map[string]*efi.EfiVar, error) {
	if j.variables == nil {
		return nil, fmt.Errorf("no variables loaded")
	}

	// Return a copy to prevent external modification
	result := make(map[string]*efi.EfiVar)
	for name, variable := range j.variables {
		result[name] = variable
	}

	return result, nil
}

// SaveChanges saves the current variables to the JSON file.
func (j *JsonEDK2Manager) SaveChanges() error {
	if j.currentMAC == nil {
		return fmt.Errorf("no MAC address loaded")
	}

	if !j.modified {
		j.logger.Info("No changes to save")
		return nil
	}

	macDir := j.macDirName(j.currentMAC)
	jsonPath := filepath.Join(j.dataDir, macDir, "fw-vars.json")

	if err := j.saveVariablesToJSON(jsonPath, j.variables); err != nil {
		return fmt.Errorf("failed to save changes: %w", err)
	}

	j.modified = false
	j.logger.Info("Changes saved", "mac", j.currentMAC.String())
	return nil
}

// RevertChanges reloads variables from the JSON file, discarding changes.
func (j *JsonEDK2Manager) RevertChanges() error {
	if j.currentMAC == nil {
		return fmt.Errorf("no MAC address loaded")
	}

	return j.LoadMAC(j.currentMAC)
}

// ResetToDefaults resets variables to default values (implementation needed).
func (j *JsonEDK2Manager) ResetToDefaults() error {
	// This would need to be implemented based on default variable requirements
	return fmt.Errorf("ResetToDefaults not yet implemented")
}

// UpdateFirmware generates a firmware binary with current variables.
func (j *JsonEDK2Manager) UpdateFirmware(firmwareData []byte) error {
	// For JSON manager, this could mean updating the base firmware
	// For now, we'll implement it as applying new firmware data
	return fmt.Errorf("UpdateFirmware not yet implemented for JSON manager")
}

// GetFirmwareVersion returns firmware version information.
func (j *JsonEDK2Manager) GetFirmwareVersion() (string, error) {
	// Extract version from variables or return a default
	if variable, exists := j.variables["PlatformLang"]; exists {
		return fmt.Sprintf("EDK2-JSON-%s", string(variable.Data)), nil
	}
	return "EDK2-JSON-Unknown", nil
}

// Boot Order Management methods would need to be implemented by parsing/manipulating
// the BootOrder, Boot#### variables similar to the original EDK2Manager

// GetBootOrder returns the current boot order.
func (j *JsonEDK2Manager) GetBootOrder() ([]string, error) {
	_, exists := j.variables["BootOrder"]
	if !exists {
		return []string{}, nil
	}

	// Parse boot order from binary data
	// Implementation would be similar to original EDK2Manager
	return []string{}, fmt.Errorf("GetBootOrder not yet fully implemented")
}

// SetBootOrder sets the boot order.
func (j *JsonEDK2Manager) SetBootOrder(order []string) error {
	// Implementation needed
	return fmt.Errorf("SetBootOrder not yet implemented")
}

// GetBootEntries returns all boot entries.
func (j *JsonEDK2Manager) GetBootEntries() ([]types.BootEntry, error) {
	// Implementation needed
	return []types.BootEntry{}, fmt.Errorf("GetBootEntries not yet implemented")
}

// AddBootEntry adds a new boot entry.
func (j *JsonEDK2Manager) AddBootEntry(entry types.BootEntry) error {
	// Implementation needed
	return fmt.Errorf("AddBootEntry not yet implemented")
}

// UpdateBootEntry updates an existing boot entry.
func (j *JsonEDK2Manager) UpdateBootEntry(id string, entry types.BootEntry) error {
	// Implementation needed
	return fmt.Errorf("UpdateBootEntry not yet implemented")
}

// DeleteBootEntry deletes a boot entry.
func (j *JsonEDK2Manager) DeleteBootEntry(id string) error {
	// Implementation needed
	return fmt.Errorf("DeleteBootEntry not yet implemented")
}

// SetBootNext sets the next boot entry.
func (j *JsonEDK2Manager) SetBootNext(index uint16) error {
	// Implementation needed
	return fmt.Errorf("SetBootNext not yet implemented")
}

// GetBootNext gets the next boot entry.
func (j *JsonEDK2Manager) GetBootNext() (uint16, error) {
	// Implementation needed
	return 0, fmt.Errorf("GetBootNext not yet implemented")
}

// Network Management methods.
func (j *JsonEDK2Manager) GetNetworkSettings() (types.NetworkSettings, error) {
	// Implementation needed
	return types.NetworkSettings{}, fmt.Errorf("GetNetworkSettings not yet implemented")
}

func (j *JsonEDK2Manager) SetNetworkSettings(settings types.NetworkSettings) error {
	// Implementation needed
	return fmt.Errorf("SetNetworkSettings not yet implemented")
}

// Boot Configuration methods.
func (j *JsonEDK2Manager) EnablePXEBoot(enable bool) error {
	// Implementation needed
	return fmt.Errorf("EnablePXEBoot not yet implemented")
}

func (j *JsonEDK2Manager) EnableHTTPBoot(enable bool) error {
	// Implementation needed
	return fmt.Errorf("EnableHTTPBoot not yet implemented")
}

func (j *JsonEDK2Manager) SetFirmwareTimeoutSeconds(seconds int) error {
	// Implementation needed
	return fmt.Errorf("SetFirmwareTimeoutSeconds not yet implemented")
}

// Device Specific Settings methods.
func (j *JsonEDK2Manager) SetConsoleConfig(consoleName string, baudRate int) error {
	// Implementation needed
	return fmt.Errorf("SetConsoleConfig not yet implemented")
}

func (j *JsonEDK2Manager) GetSystemInfo() (types.SystemInfo, error) {
	// Implementation needed
	return types.SystemInfo{}, fmt.Errorf("GetSystemInfo not yet implemented")
}
