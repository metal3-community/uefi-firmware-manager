package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/bmcpi/uefi-firmware-manager/efi"
	"github.com/bmcpi/uefi-firmware-manager/manager"
	"github.com/go-logr/logr"
)

func main() {
	// Set up logger
	logger := logr.Discard()

	// Get the path to the firmware file
	firmwarePath := filepath.Join("edk2", "RPI_EFI.fd")

	// Check if firmware file exists
	if _, err := os.Stat(firmwarePath); os.IsNotExist(err) {
		log.Fatalf("Firmware file not found: %s", firmwarePath)
	}

	// Create EDK2 manager
	mgr, err := manager.NewEDK2Manager(firmwarePath, logger)
	if err != nil {
		log.Fatalf("Failed to create EDK2 manager: %v", err)
	}

	// Create the Boot0099 variable
	err = createBoot0099Variable(mgr)
	if err != nil {
		log.Fatalf("Failed to create Boot0099 variable: %v", err)
	}

	// Set BootNext to 0099
	err = setBootNext(mgr)
	if err != nil {
		log.Fatalf("Failed to set BootNext: %v", err)
	}

	// Save changes
	err = mgr.SaveChanges()
	if err != nil {
		log.Fatalf("Failed to save changes: %v", err)
	}

	fmt.Println("Successfully created Boot0099 variable and set BootNext")
}

func createBoot0099Variable(mgr *manager.EDK2Manager) error {
	// Create the device path: MAC()/IPv4()
	macAddr := net.HardwareAddr{0xd8, 0x3a, 0xdd, 0x61, 0x4d, 0x15} // d83add614d15
	devPath := &efi.DevicePath{}
	devPath = devPath.Mac(macAddr).IPv4()

	// Create the title as UCS16String
	title := efi.NewUCS16String("UEFI PXEv4 (MAC:D83ADD614D15)")

	// Create the boot entry
	bootEntry := &efi.BootEntry{
		Attr:       efi.LOAD_OPTION_ACTIVE, // LOAD_OPTION_ACTIVE
		Title:      *title,
		DevicePath: *devPath,
		OptData:    mustDecodeHex("4eac0881119f594d850ee21a522c59b2"),
	}

	// Create the EFI variable
	efiVar := &efi.EfiVar{
		Name: efi.FromString("Boot0099"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess, // attr = 7
		Data: bootEntry.Bytes(),
	}

	// Set the variable
	return mgr.SetVariable("Boot0099", efiVar)
}

func setBootNext(mgr *manager.EDK2Manager) error {
	// Create BootNext variable with value 0x0099 (little endian)
	bootNextData := []byte{0x99, 0x00}

	// Create the EFI variable
	efiVar := &efi.EfiVar{
		Name: efi.FromString("BootNext"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess, // attr = 7
		Data: bootNextData,
	}

	// Set the variable
	return mgr.SetVariable("BootNext", efiVar)
}

func mustDecodeHex(s string) []byte {
	data, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("Failed to decode hex string %s: %v", s, err))
	}
	return data
}
