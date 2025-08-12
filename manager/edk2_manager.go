package manager

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bmcpi/uefi-firmware-manager/edk2"
	"github.com/bmcpi/uefi-firmware-manager/efi"
	"github.com/bmcpi/uefi-firmware-manager/types"
	"github.com/bmcpi/uefi-firmware-manager/varstore"
	"github.com/go-logr/logr"
)

// EDK2Manager implements the FirmwareManager interface for Raspberry Pi EDK2 firmware.
type EDK2Manager struct {
	firmwarePath string
	varStore     *varstore.Edk2VarStore
	varList      efi.EfiVarList
	logger       logr.Logger
}

// NewEDK2Manager creates a new EDK2Manager for the given firmware file.
func NewEDK2Manager(firmwarePath string, logger logr.Logger) (FirmwareManager, error) {
	manager := &EDK2Manager{
		firmwarePath: firmwarePath,
		logger:       logger.WithName("edk2-manager"),
	}

	if _, err := os.Stat(firmwarePath); os.IsNotExist(err) {

		firmwareRoot := filepath.Dir(firmwarePath)

		if err := os.MkdirAll(firmwareRoot, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create firmware directory: %w", err)
		}

		for k, f := range edk2.Files {
			kf := filepath.Join(firmwareRoot, k)
			kfr := filepath.Dir(kf)

			if kfr != firmwareRoot {
				if err := os.MkdirAll(kfr, 0o755); err != nil {
					return nil, fmt.Errorf("failed to create firmware directory: %w", err)
				}
			}

			if err := os.WriteFile(kf, f, 0o644); err != nil {
				return nil, fmt.Errorf("failed to create firmware file: %w", err)
			}
		}
	}

	// Initialize the variable store
	manager.varStore = varstore.NewEdk2VarStore(firmwarePath)
	manager.varStore.Logger = logger.WithName("edk2-varstore")

	// Load the variable list
	var err error
	manager.varList, err = manager.varStore.GetVarList()
	if err != nil {
		return nil, fmt.Errorf("failed to get variable list: %w", err)
	}

	return manager, nil
}

// GetBootOrder retrieves the boot order as a list of entry IDs.
func (m *EDK2Manager) GetBootOrder() ([]string, error) {
	bootOrderVar, found := m.varList[efi.BootOrder]
	if !found {
		return []string{}, nil
	}

	bootSequence, err := bootOrderVar.GetBootOrder()
	if err != nil {
		return nil, fmt.Errorf("failed to parse boot order: %w", err)
	}

	result := make([]string, len(bootSequence))
	for i, id := range bootSequence {
		result[i] = fmt.Sprintf("%04X", id)
	}

	return result, nil
}

func (m *EDK2Manager) SetBootNext(index uint16) error {
	return m.varList.SetBootNext(index)
}

func (m *EDK2Manager) SetBootLast(entry types.BootEntry) error {
	bootEntryName := "Boot0099"
	// Create or update the boot entry variable
	bootEntryVar := &efi.EfiVar{
		Name: efi.NewUCS16String(bootEntryName),
		Guid: efi.StringToGUID(efi.EFI_GLOBAL_VARIABLE),
		Attr: efi.EFI_VARIABLE_NON_VOLATILE | efi.EFI_VARIABLE_BOOTSERVICE_ACCESS | efi.EFI_VARIABLE_RUNTIME_ACCESS,
	}
	optData := []byte{}
	if len(entry.OptData) != 0 {
		odata, err := hex.DecodeString(entry.OptData)
		if err != nil && entry.OptData != "" {
			return fmt.Errorf("invalid optional data format: %w", err)
		}
		optData = odata
	}

	// Set the boot entry with the specified title and device path
	err := bootEntryVar.SetBootEntry(1, entry.Name, entry.DevPath, optData)
	if err != nil {
		return fmt.Errorf("failed to set boot entry: %w", err)
	}

	// Add the entry to the variable list
	m.varList[bootEntryName] = bootEntryVar

	return nil
}

func (m *EDK2Manager) GetBootLast() (*types.BootEntry, error) {
	if bootEntryVar, found := m.varList["Boot0099"]; found {
		bootEntry, err := bootEntryVar.GetBootEntry()
		if err != nil {
			return nil, fmt.Errorf("failed to get boot entry: %w", err)
		}
		return &types.BootEntry{
			ID:      fmt.Sprintf("%04X", 99),
			Name:    bootEntry.Title.String(),
			DevPath: bootEntry.DevicePath.String(),
			Enabled: (bootEntry.Attr & efi.LOAD_OPTION_ACTIVE) != 0,
			OptData: hex.EncodeToString(bootEntry.OptData),
		}, nil
	}
	return nil, fmt.Errorf("boot entry not found")
}

func (m *EDK2Manager) GetBootNext() (uint16, error) {
	bootNextVar, found := m.varList[efi.BootNext]
	if !found {
		return 0, nil
	}
	return bootNextVar.GetBootNext()
}

func (m *EDK2Manager) DeleteBootNext() error {
	return m.DeleteVariable(efi.BootNext)
}

// SetBootOrder sets the boot order from a list of entry IDs.
func (m *EDK2Manager) SetBootOrder(order []string) error {
	bootSequence := make([]uint16, len(order))

	for i, id := range order {
		// Remove "Boot" prefix if present
		id = strings.TrimPrefix(id, "Boot")

		// Parse the hex entry ID
		entryID, err := strconv.ParseUint(id, 16, 16)
		if err != nil {
			return fmt.Errorf("invalid boot entry ID '%s': %w", id, err)
		}

		bootSequence[i] = uint16(entryID)
	}

	// Get or create the BootOrder variable
	bootOrderVar, found := m.varList[efi.BootOrder]
	if !found {
		bootOrderVar = &efi.EfiVar{
			Name: efi.NewUCS16String(efi.BootOrder),
			Guid: efi.StringToGUID(efi.EFI_GLOBAL_VARIABLE),
			Attr: efi.EFI_VARIABLE_NON_VOLATILE |
				efi.EFI_VARIABLE_BOOTSERVICE_ACCESS |
				efi.EFI_VARIABLE_RUNTIME_ACCESS,
		}
		m.varList[efi.BootOrder] = bootOrderVar
	}

	// Set the new boot order
	bootOrderVar.SetBootOrder(bootSequence)

	return nil
}

// GetBootEntries returns all boot entries from the firmware.
func (m *EDK2Manager) GetBootEntries() ([]types.BootEntry, error) {
	bootEntries, err := m.varList.ListBootEntries()
	if err != nil {
		return nil, fmt.Errorf("failed to list boot entries: %w", err)
	}

	// Convert to the public types.BootEntry type
	result := make([]types.BootEntry, 0, len(bootEntries))
	for id, entry := range bootEntries {
		// Skip empty entries
		if entry == nil {
			continue
		}

		position := 0
		enabled := (entry.Attr & efi.LOAD_OPTION_ACTIVE) != 0

		// Get position from boot order
		bootOrderVar, found := m.varList[efi.BootOrder]
		if found {
			bootSequence, err := bootOrderVar.GetBootOrder()
			if err == nil {
				for i, bootID := range bootSequence {
					if bootID == id {
						position = i
						break
					}
				}
			}
		}

		bootEntry := types.BootEntry{
			ID:       fmt.Sprintf("%04X", id),
			Name:     entry.Title.String(),
			DevPath:  entry.DevicePath.String(),
			Enabled:  enabled,
			Position: position,
		}

		result = append(result, bootEntry)
	}

	return result, nil
}

// AddBootEntry adds a new boot entry to the firmware.
func (m *EDK2Manager) AddBootEntry(entry types.BootEntry) error {
	foundKey := false
	// Find the next available boot entry ID
	maxID := uint16(0)
	for k := range m.varList {
		if strings.HasPrefix(k, efi.BootPrefix) && len(k) == 8 {
			foundKey = true
			idStr := k[4:] // Extract the ID portion
			id, err := strconv.ParseUint(idStr, 16, 16)
			if err == nil && uint16(id) > maxID {
				maxID = uint16(id)
			}
		}
	}
	nextID := maxID + 1
	if !foundKey {
		nextID = 0
	}

	// Create the boot entry name
	bootEntryName := fmt.Sprintf("%s%04X", efi.BootPrefix, nextID)

	// Create or update the boot entry variable
	bootEntryVar := &efi.EfiVar{
		Name: efi.NewUCS16String(bootEntryName),
		Guid: efi.StringToGUID(efi.EFI_GLOBAL_VARIABLE),
		Attr: efi.EFI_VARIABLE_NON_VOLATILE | efi.EFI_VARIABLE_BOOTSERVICE_ACCESS | efi.EFI_VARIABLE_RUNTIME_ACCESS,
	}

	// Set attributes based on enabled status
	attr := uint32(0)
	if entry.Enabled {
		attr |= efi.LOAD_OPTION_ACTIVE
	}

	var err error

	optData := []byte{}
	if len(entry.OptData) != 0 {
		optData, err = hex.DecodeString(entry.OptData)
		if err != nil && entry.OptData != "" {
			return fmt.Errorf("invalid optional data format: %w", err)
		}
	}

	// Set the boot entry with the specified title and device path
	err = bootEntryVar.SetBootEntry(attr, entry.Name, entry.DevPath, optData)
	if err != nil {
		return fmt.Errorf("failed to set boot entry: %w", err)
	}

	// Add the entry to the variable list
	m.varList[bootEntryName] = bootEntryVar

	// Update the boot order if position is specified
	if entry.Position >= 0 {
		bootOrder, err := m.GetBootOrder()
		if err != nil {
			return fmt.Errorf("failed to get boot order: %w", err)
		}

		// Convert the new ID to a string format matching the boot order
		newEntryID := fmt.Sprintf("%04X", nextID)

		// Insert the new entry at the specified position
		if entry.Position >= len(bootOrder) {
			bootOrder = append(bootOrder, newEntryID)
		} else {
			bootOrder = append(bootOrder[:entry.Position], append([]string{newEntryID}, bootOrder[entry.Position:]...)...)
		}

		// Update the boot order
		if err := m.SetBootOrder(bootOrder); err != nil {
			return fmt.Errorf("failed to update boot order: %w", err)
		}
	}

	return nil
}

// UpdateBootEntry updates an existing boot entry in the firmware.
func (m *EDK2Manager) UpdateBootEntry(id string, entry types.BootEntry) error {
	// Add "Boot" prefix if not present
	if !strings.HasPrefix(id, efi.BootPrefix) {
		id = efi.BootPrefix + id
	}

	// Check if the entry exists
	bootEntryVar, found := m.varList[id]
	if !found {
		return fmt.Errorf("boot entry not found: %s", id)
	}

	// Get the current boot entry
	currentEntry, err := bootEntryVar.GetBootEntry()
	if err != nil {
		return fmt.Errorf("failed to parse boot entry: %w", err)
	}

	// Set attributes based on enabled status
	attr := currentEntry.Attr
	if entry.Enabled {
		attr |= efi.LOAD_OPTION_ACTIVE
	} else {
		attr &= ^uint32(efi.LOAD_OPTION_ACTIVE)
	}

	// Update the boot entry
	err = bootEntryVar.SetBootEntry(attr, entry.Name, entry.DevPath, currentEntry.OptData)
	if err != nil {
		return fmt.Errorf("failed to update boot entry: %w", err)
	}

	// Update the boot order if position is specified
	if entry.Position >= 0 {
		// Extract numeric ID from the boot entry
		idStr := strings.TrimPrefix(id, efi.BootPrefix)
		bootEntryID, err := strconv.ParseUint(idStr, 16, 16)
		if err != nil {
			return fmt.Errorf("invalid boot entry ID: %w", err)
		}

		bootOrder, err := m.GetBootOrder()
		if err != nil {
			return fmt.Errorf("failed to get boot order: %w", err)
		}

		// Find and remove the entry from the current boot order
		entryIndex := -1
		entryIDStr := fmt.Sprintf("%04X", bootEntryID)
		for i, orderID := range bootOrder {
			if orderID == entryIDStr {
				entryIndex = i
				break
			}
		}

		if entryIndex >= 0 {
			bootOrder = append(bootOrder[:entryIndex], bootOrder[entryIndex+1:]...)
		}

		// Insert the entry at the new position
		if entry.Position >= len(bootOrder) {
			bootOrder = append(bootOrder, entryIDStr)
		} else {
			bootOrder = append(bootOrder[:entry.Position], append([]string{entryIDStr}, bootOrder[entry.Position:]...)...)
		}

		// Update the boot order
		if err := m.SetBootOrder(bootOrder); err != nil {
			return fmt.Errorf("failed to update boot order: %w", err)
		}
	}

	return nil
}

// DeleteBootEntry deletes a boot entry from the firmware.
func (m *EDK2Manager) DeleteBootEntry(id string) error {
	// Add "Boot" prefix if not present
	if !strings.HasPrefix(id, efi.BootPrefix) {
		id = efi.BootPrefix + id
	}

	// Check if the entry exists
	_, found := m.varList[id]
	if !found {
		return fmt.Errorf("boot entry not found: %s", id)
	}

	// Remove the entry from the boot order
	bootOrder, err := m.GetBootOrder()
	if err != nil {
		return fmt.Errorf("failed to get boot order: %w", err)
	}

	// Extract numeric ID from the boot entry
	idStr := strings.TrimPrefix(id, efi.BootPrefix)

	// Remove the entry from the boot order
	newBootOrder := make([]string, 0, len(bootOrder))
	for _, orderID := range bootOrder {
		if orderID != idStr {
			newBootOrder = append(newBootOrder, orderID)
		}
	}

	// Update the boot order
	if err := m.SetBootOrder(newBootOrder); err != nil {
		return fmt.Errorf("failed to update boot order: %w", err)
	}

	// Delete the entry from the variable list
	delete(m.varList, id)

	return nil
}

// GetNetworkSettings returns the current network settings.
func (m *EDK2Manager) GetNetworkSettings() (types.NetworkSettings, error) {
	settings := types.NetworkSettings{
		EnableDHCP: true, // Default to DHCP enabled
	}

	// Get MAC address
	macAddr, err := m.GetMacAddress()
	if err == nil && macAddr != nil {
		settings.MacAddress = macAddr.String()
	}

	// Get IPv6 enabled setting
	ipv6Var, found := m.varList["IPv6Support"]
	if found {
		ipv6Enabled, err := ipv6Var.GetUint32()
		if err == nil {
			settings.EnableIPv6 = ipv6Enabled != 0
		}
	}

	// Get VLAN settings
	vlanVar, found := m.varList["VLANEnable"]
	if found {
		vlanEnabled, err := vlanVar.GetUint32()
		if err == nil {
			settings.VLANEnabled = vlanEnabled != 0
		}
	}

	vlanIDVar, found := m.varList["VLANID"]
	if found {
		vlanID, err := vlanIDVar.GetUint32()
		if err == nil {
			settings.VLANID = fmt.Sprintf("%d", vlanID)
		}
	}

	return settings, nil
}

// SetNetworkSettings sets the network settings.
func (m *EDK2Manager) SetNetworkSettings(settings types.NetworkSettings) error {
	// Set MAC address if provided
	if settings.MacAddress != "" {
		mac, err := net.ParseMAC(settings.MacAddress)
		if err != nil {
			return fmt.Errorf("invalid MAC address: %w", err)
		}

		if err := m.SetMacAddress(mac); err != nil {
			return fmt.Errorf("failed to set MAC address: %w", err)
		}
	}

	// Set IPv6 support
	ipv6Var := m.getOrCreateVar("IPv6Support", efi.EFI_GLOBAL_VARIABLE)
	ipv6Var.SetUint32(boolToUint32(settings.EnableIPv6))

	// Set VLAN settings
	vlanVar := m.getOrCreateVar("VLANEnable", efi.EFI_GLOBAL_VARIABLE)
	vlanVar.SetUint32(boolToUint32(settings.VLANEnabled))

	if settings.VLANEnabled && settings.VLANID != "" {
		vlanID, err := strconv.ParseUint(settings.VLANID, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid VLAN ID: %w", err)
		}

		vlanIDVar := m.getOrCreateVar("VLANID", efi.EFI_GLOBAL_VARIABLE)
		vlanIDVar.SetUint32(uint32(vlanID))
	}

	return nil
}

// GetMacAddress retrieves the MAC address from the firmware.
func (m *EDK2Manager) GetMacAddress() (net.HardwareAddr, error) {
	// Look for MAC address in boot entries
	entries, err := m.GetBootEntries()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot entries: %w", err)
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name, "MAC:") {
			macIndex := strings.Index(entry.Name, "MAC:")
			if macIndex >= 0 {
				macStr := entry.Name[macIndex+4:]
				macEnd := strings.Index(macStr, ")")
				if macEnd >= 0 {
					macStr = macStr[:macEnd]
				}

				// Try to parse the MAC address
				mac, err := net.ParseMAC(macStr)
				if err == nil {
					return mac, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("MAC address not found")
}

// SetMacAddress sets the MAC address in the firmware.
func (m *EDK2Manager) SetMacAddress(mac net.HardwareAddr) error {
	var err error

	devPath := &efi.DevicePath{}
	devPath = devPath.Mac(mac).IPv4()

	stitle := fmt.Sprintf("UEFI PXEv4 (MAC:%s)", mac.String())

	// Create the title as UCS16String
	title := efi.NewUCS16String(stitle)
	optData, err := hex.DecodeString("4eac0881119f594d850ee21a522c59b2")
	if err != nil {
		return fmt.Errorf("failed to decode OptData: %w", err)
	}

	// Create the boot entry
	bootEntry := &efi.BootEntry{
		Attr:       efi.LOAD_OPTION_ACTIVE, // LOAD_OPTION_ACTIVE
		Title:      *title,
		DevicePath: *devPath,
		OptData:    optData,
	}

	// Set the variable
	if err := m.SetVariable("Boot0099", &efi.EfiVar{
		Name: efi.FromString("Boot0099"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess, // attr = 7
		Data: bootEntry.Bytes(),
	}); err != nil {
		return fmt.Errorf("failed to set Boot0099 variable: %w", err)
	}

	// Set the variable
	return m.SetVariable("BootNext", &efi.EfiVar{
		Name: efi.FromString("BootNext"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess, // attr = 7
		Data: []byte{0x99, 0x00},
	})
}

// GetVariable retrieves a variable by name.
func (m *EDK2Manager) GetVariable(name string) (*efi.EfiVar, error) {
	v, found := m.varList[name]
	if !found {
		return nil, fmt.Errorf("variable not found: %s", name)
	}
	return v, nil
}

// DeleteVariable removes a variable by name.
func (m *EDK2Manager) DeleteVariable(name string) error {
	if _, found := m.varList[name]; !found {
		return fmt.Errorf("variable not found: %s", name)
	}
	delete(m.varList, name)
	return nil
}

// GetVarList retrieves the list of all variables.
func (m *EDK2Manager) GetVarList() (efi.EfiVarList, error) {
	return m.varList, nil
}

// GetVariableAsType retrieves a variable and converts it to a structured Go type based on its characteristics.
func (m *EDK2Manager) GetVariableAsType(name string) (any, error) {
	v, found := m.varList[name]
	if !found {
		return nil, fmt.Errorf("variable not found: %s", name)
	}

	// Identify the variable type based on name patterns and GUID
	return m.identifyAndConvertVariable(name, v)
}

// identifyAndConvertVariable identifies the type of EFI variable and converts it to appropriate Go type.
func (m *EDK2Manager) identifyAndConvertVariable(name string, v *efi.EfiVar) (any, error) {
	guidStr := v.Guid.String()

	// Check for MAC address-based IPv6 configuration (12-character hex MAC addresses)
	if len(name) == 12 && isMACAddress(name) && guidStr == efi.EfiIp6ConfigProtocol {
		ip6Config, err := efi.NewIp6ConfigData(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse IPv6 config data: %w", err)
		}
		return ip6Config, nil
	}

	// Network Device List
	if name == "_NDL" {
		deviceList, err := efi.NewNetworkDeviceList(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse network device list: %w", err)
		}
		return deviceList, nil
	}

	// DHCP6 Client ID
	if name == "ClientId" && guidStr == efi.EfiDhcp6ServiceBindingProtocol {
		clientId, err := efi.NewDhcp6Duid(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DHCP6 client ID: %w", err)
		}
		return clientId, nil
	}

	// Platform Configuration
	if name == "Setup" {
		platformConfig := efi.NewPlatformConfig()
		// Platform config doesn't have raw data parsing - would need specific implementation
		return platformConfig, nil
	}

	// Console Configuration
	if name == "ConsolePref" {
		consoleConfig := efi.NewConsoleConfig()
		// Console config doesn't have raw data parsing - would need specific implementation
		return consoleConfig, nil
	}

	// Security Configuration
	if name == "SecureBoot" || name == "VendorKeysNv" {
		securityConfig := efi.NewSecurityConfig()
		// Security config doesn't have raw data parsing - would need specific implementation
		return securityConfig, nil
	}

	// Time Configuration
	if name == "Time" || name == "Timezone" {
		timeConfig := efi.NewTimeConfig()
		// Time config doesn't have raw data parsing - would need specific implementation
		return timeConfig, nil
	}

	// iSCSI Configuration
	if name == "ISCSIBootData" {
		// iSCSI config needs specific implementation based on data format
		return nil, fmt.Errorf("iSCSI config parsing not yet implemented")
	}

	// Key Data (enrollment keys, certificates)
	if name == "PK" || name == "KEK" || name == "db" || name == "dbx" {
		keyData, err := efi.NewKeyData(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse key data: %w", err)
		}
		return keyData, nil
	}

	// Asset Tag
	if name == "AssetTag" {
		assetTag, err := efi.NewAssetTag(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse asset tag: %w", err)
		}
		return assetTag, nil
	}

	// Certificate Database
	if name == "certdb" {
		certDb, err := efi.NewCertDatabase(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate database: %w", err)
		}
		return certDb, nil
	}

	// For unrecognized types, return the raw EfiVar
	return v, nil
}

// ListVariablesWithTypes returns all variables with their converted Go types.
func (m *EDK2Manager) ListVariablesWithTypes() (map[string]any, error) {
	result := make(map[string]any)

	for name, v := range m.varList {
		convertedVar, err := m.identifyAndConvertVariable(name, v)
		if err != nil {
			// If conversion fails, store the raw variable with error info
			result[name] = map[string]any{
				"raw_variable":     v,
				"conversion_error": err.Error(),
			}
		} else {
			result[name] = convertedVar
		}
	}

	return result, nil
}

// SetVariableFromType sets a variable from a structured Go type.
func (m *EDK2Manager) SetVariableFromType(name string, value any) error {
	// For now, only support direct EfiVar assignment since ToBytes methods aren't implemented
	switch v := value.(type) {
	case *efi.EfiVar:
		// Direct EfiVar assignment
		m.varList[name] = v
		return nil
	default:
		return fmt.Errorf("unsupported variable type for direct assignment: %T. Only *efi.EfiVar is currently supported", value)
	}
}

var macRegex = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`)

// isMACAddress checks if a string represents a valid MAC address (12 hex characters).
func isMACAddress(s string) bool {
	return macRegex.MatchString(s)
}

// SetVariable sets a variable.
func (m *EDK2Manager) SetVariable(name string, value *efi.EfiVar) error {
	if value == nil {
		return fmt.Errorf("variable is nil")
	}
	m.varList[name] = value
	return nil
}

// ListVariables returns all variables in the firmware.
func (m *EDK2Manager) ListVariables() (map[string]*efi.EfiVar, error) {
	return m.varList, nil
}

// EnablePXEBoot enables or disables PXE boot.
func (m *EDK2Manager) EnablePXEBoot(enable bool) error {
	// Get all boot entries
	entries, err := m.GetBootEntries()
	if err != nil {
		return fmt.Errorf("failed to get boot entries: %w", err)
	}

	// Find PXE boot entries
	pxeEntries := make([]types.BootEntry, 0)
	for _, entry := range entries {
		if strings.Contains(entry.Name, "PXE") {
			entry.Enabled = enable
			pxeEntries = append(pxeEntries, entry)
		}
	}

	// Update PXE boot entries
	for _, entry := range pxeEntries {
		if err := m.UpdateBootEntry(entry.ID, entry); err != nil {
			return fmt.Errorf("failed to update PXE boot entry %s: %w", entry.ID, err)
		}
	}

	// If we need to enable PXE and no entries were found, create one
	if enable && len(pxeEntries) == 0 {
		mac, err := m.GetMacAddress()
		if err != nil {
			mac = net.HardwareAddr{0, 0, 0, 0, 0, 0}
		}

		macStr := strings.ToUpper(strings.ReplaceAll(mac.String(), ":", ""))

		// Create IPv4 PXE entry
		pxeEntry := types.BootEntry{
			Name:     fmt.Sprintf("UEFI PXEv4 (MAC:%s)", macStr),
			DevPath:  "MAC()/IPv4()",
			Enabled:  true,
			Position: 0, // Set as first boot option
		}

		if err := m.AddBootEntry(pxeEntry); err != nil {
			return fmt.Errorf("failed to add PXE boot entry: %w", err)
		}
	}

	return nil
}

// EnableHTTPBoot enables or disables HTTP boot.
func (m *EDK2Manager) EnableHTTPBoot(enable bool) error {
	// Get all boot entries
	entries, err := m.GetBootEntries()
	if err != nil {
		return fmt.Errorf("failed to get boot entries: %w", err)
	}

	// Find HTTP boot entries
	httpEntries := make([]types.BootEntry, 0)
	for _, entry := range entries {
		if strings.Contains(entry.Name, "HTTP") {
			entry.Enabled = enable
			httpEntries = append(httpEntries, entry)
		}
	}

	// Update HTTP boot entries
	for _, entry := range httpEntries {
		if err := m.UpdateBootEntry(entry.ID, entry); err != nil {
			return fmt.Errorf("failed to update HTTP boot entry %s: %w", entry.ID, err)
		}
	}

	// If we need to enable HTTP boot and no entries were found, create one
	if enable && len(httpEntries) == 0 {
		mac, err := m.GetMacAddress()
		if err != nil {
			mac = net.HardwareAddr{0, 0, 0, 0, 0, 0}
		}

		macStr := strings.ToUpper(strings.ReplaceAll(mac.String(), ":", ""))

		// Create IPv4 HTTP entry
		httpEntry := types.BootEntry{
			Name:     fmt.Sprintf("UEFI HTTPv4 (MAC:%s)", macStr),
			DevPath:  "MAC()/IPv4()/URI()",
			Enabled:  true,
			Position: 1, // Set as second boot option
		}

		if err := m.AddBootEntry(httpEntry); err != nil {
			return fmt.Errorf("failed to add HTTP boot entry: %w", err)
		}
	}

	return nil
}

// SetFirmwareTimeoutSeconds sets the boot menu timeout in seconds.
func (m *EDK2Manager) SetFirmwareTimeoutSeconds(seconds int) error {
	// The timeout is stored as a 16-bit value in the Timeout variable
	timeoutVar := m.getOrCreateVar("Timeout", efi.EFI_GLOBAL_VARIABLE)

	// Convert seconds to the format expected by the firmware
	data := []byte{byte(seconds & 0xFF), byte((seconds >> 8) & 0xFF)}
	timeoutVar.Data = data

	return nil
}

// SetConsoleConfig sets the console configuration.
func (m *EDK2Manager) SetConsoleConfig(consoleName string, baudRate int) error {
	// Update the console preference variable
	consoleVar := m.getOrCreateVar("ConsolePref", "2d2358b4-e96c-484d-b2dd-7c2edfc7d56f")

	// Set console preference based on name
	var prefValue uint32
	switch strings.ToLower(consoleName) {
	case "serial":
		prefValue = 1
	case "graphics":
		prefValue = 2
	default:
		prefValue = 0 // Auto
	}

	consoleVar.SetUint32(prefValue)

	// Update baud rate if serial console is selected
	if prefValue == 1 && baudRate > 0 {
		baudVar := m.getOrCreateVar("SerialBaudRate", "cd7cc258-31db-22e6-9f22-63b0b8eed6b5")
		baudVar.SetUint32(uint32(baudRate))
	}

	return nil
}

// GetSystemInfo returns information about the system.
func (m *EDK2Manager) GetSystemInfo() (types.SystemInfo, error) {
	info := types.SystemInfo{}

	// Add firmware version
	version, err := m.GetFirmwareVersion()
	if err == nil {
		info["FirmwareVersion"] = version
	}

	// Try to get asset tag
	assetVar, found := m.varList["AssetTag"]
	if found {
		info["AssetTag"] = string(assetVar.Data)
	}

	// Get CPU settings
	cpuVar, found := m.varList["CpuClock"]
	if found {
		cpuVal, err := cpuVar.GetUint32()
		if err == nil {
			info["CpuClock"] = fmt.Sprintf("%d", cpuVal)
		}
	}

	// Add RAM information
	ramVar, found := m.varList["RamMoreThan3GB"]
	if found {
		ramVal, err := ramVar.GetUint32()
		if err == nil {
			if ramVal != 0 {
				info["RAM"] = "More than 3GB"
			} else {
				info["RAM"] = "3GB or less"
			}
		}
	}

	// Add system table mode
	sysTableVar, found := m.varList["SystemTableMode"]
	if found {
		sysTableVal, err := sysTableVar.GetUint32()
		if err == nil {
			info["SystemTableMode"] = fmt.Sprintf("%d", sysTableVal)
		}
	}

	return info, nil
}

// UpdateFirmware updates the firmware with the provided data.
func (m *EDK2Manager) UpdateFirmware(firmwareData []byte) error {
	// Backup the original firmware
	backupPath := m.firmwarePath + ".backup"
	if err := copyFile(m.firmwarePath, backupPath); err != nil {
		return fmt.Errorf("failed to backup firmware: %w", err)
	}

	defer func() { _ = removeFile(backupPath) }()

	err := m.varStore.WriteVarStore(m.firmwarePath, m.varList)
	if err != nil {
		// Restore from backup if write fails
		if restoreErr := copyFile(backupPath, m.firmwarePath); restoreErr != nil {
			m.logger.Error(restoreErr, "failed to restore firmware from backup")
		}
		return fmt.Errorf("failed to write variable store: %w", err)
	}

	m.logger.Info("firmware updated successfully", "path", m.firmwarePath)

	return nil
}

// GetFirmwareVersion returns the firmware version.
func (m *EDK2Manager) GetFirmwareVersion() (string, error) {
	// Try to extract version from embedded firmware info
	var version string

	// Get the data from the FirmwareRevision variable if it exists
	revVar, found := m.varList["FirmwareRevision"]
	if found {
		version = string(revVar.Data)
	}

	// If no version found, use the firmware file modification time
	if version == "" {
		fileInfo, err := getFileInfo(m.firmwarePath)
		if err == nil {
			modTime := fileInfo.ModTime()
			version = fmt.Sprintf("Unknown (Modified: %s)", modTime.Format(time.RFC3339))
		} else {
			version = "Unknown"
		}
	}

	return version, nil
}

// SaveChanges writes the modified variables back to the firmware file.
func (m *EDK2Manager) SaveChanges() error {
	if err := m.varStore.WriteVarStore(m.firmwarePath, m.varList); err != nil {
		return fmt.Errorf("failed to write variable store: %w", err)
	}

	m.logger.Info("firmware saved successfully", "path", m.firmwarePath)

	return nil
}

// RevertChanges discards all changes.
func (m *EDK2Manager) RevertChanges() error {
	// Reload the variables from the file
	var err error
	m.varList, err = m.varStore.GetVarList()
	if err != nil {
		return fmt.Errorf("failed to reload variable list: %w", err)
	}

	return nil
}

// ResetToDefaults resets the firmware to default settings.
func (m *EDK2Manager) ResetToDefaults() error {
	// Reset the boot timeout
	timeoutVar := m.getOrCreateVar("Timeout", efi.EFI_GLOBAL_VARIABLE)
	timeoutVar.Data = []byte{0x05, 0x00} // 5 seconds

	// Reset console preference
	consoleVar := m.getOrCreateVar("ConsolePref", "2d2358b4-e96c-484d-b2dd-7c2edfc7d56f")
	consoleVar.SetUint32(0) // Auto

	// Reset the boot order to defaults
	defaultBootOrder := []string{"0000", "0001"} // UiApp, SD/MMC
	if err := m.SetBootOrder(defaultBootOrder); err != nil {
		return fmt.Errorf("failed to reset boot order: %w", err)
	}

	// Reset network settings
	ipv6Var := m.getOrCreateVar("IPv6Support", efi.EFI_GLOBAL_VARIABLE)
	ipv6Var.SetUint32(0) // Disable IPv6

	vlanVar := m.getOrCreateVar("VLANEnable", efi.EFI_GLOBAL_VARIABLE)
	vlanVar.SetUint32(0) // Disable VLAN

	return nil
}

// Helper functions.

// getOrCreateVar gets an existing variable or creates a new one with the specified name and GUID.
func (m *EDK2Manager) getOrCreateVar(name, guidStr string) *efi.EfiVar {
	v, found := m.varList[name]
	if found {
		return v
	}

	// Create a new variable
	v = &efi.EfiVar{
		Name: efi.NewUCS16String(name),
		Guid: efi.StringToGUID(guidStr),
		Attr: efi.EFI_VARIABLE_NON_VOLATILE |
			efi.EFI_VARIABLE_BOOTSERVICE_ACCESS |
			efi.EFI_VARIABLE_RUNTIME_ACCESS,
	}
	m.varList[name] = v

	return v
}

// boolToUint32 converts a boolean to a uint32 (0 or 1).
func boolToUint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// File utility functions.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", src, err)
	}
	return os.WriteFile(dst, data, 0o644)
}

func removeFile(path string) error {
	return os.Remove(path)
}

func getFileInfo(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
