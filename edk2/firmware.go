package edk2

import (
	_ "embed"
	"fmt"
	"net"

	"github.com/metal3-community/uefi-firmware-manager/efi"
	"github.com/metal3-community/uefi-firmware-manager/varstore"
)

const FirmwareFileName = "RPI_EFI.fd"

// RpiEfi returns the RPI_EFI.fd file.
//
//go:embed RPI_EFI.fd
var RpiEfi []byte

// FixupDat returns the fixup.dat file.
//
//go:embed fixup4.dat
var Fixup4Dat []byte

// Start4ElfDat returns the start4.elf file.
//
//go:embed start4.elf
var Start4ElfDat []byte

// Bcm2711Rpi4BDtb returns the bcm2711-rpi-4-b.dtb file.
//
//go:embed bcm2711-rpi-4-b.dtb
var Bcm2711Rpi4BDtb []byte

// Bcm2711Rpi400Dtb returns the bcm2711-rpi-400.dtb file.
//
//go:embed bcm2711-rpi-400.dtb
var Bcm2711Rpi400Dtb []byte

// Bcm2711RpiCm4Dtb returns the bcm2711-rpi-cm4.dtb file.
//
//go:embed bcm2711-rpi-cm4.dtb
var Bcm2711RpiCm4Dtb []byte

// OverlaysMiniUartBtDtbo returns the overlays/miniuart-bt.dtbo file.
//
//go:embed overlays/miniuart-bt.dtbo
var OverlaysMiniUartBtDtbo []byte

// OverlaysUpstreamPi4Dtbo returns the overlays/upstream-pi4.dtbo file.
//
//go:embed overlays/upstream-pi4.dtbo
var OverlaysUpstreamPi4Dtbo []byte

// OverlaysRpiPoePlusDtbo returns the overlays/rpi-poe-plus.dtbo file.
//
//go:embed overlays/rpi-poe-plus.dtbo
var OverlaysRpiPoePlusDtbo []byte

// FirmwareBrcmBrcmfmac43455SdioBin returns the firmware/brcm/brcmfmac43455-sdio.bin file.
//
//go:embed firmware/brcm/brcmfmac43455-sdio.bin
var FirmwareBrcmBrcmfmac43455SdioBin []byte

// FirmwareBrcmBrcmfmac43455SdioTxt returns the firmware/brcm/brcmfmac43455-sdio.txt file.
//
//go:embed firmware/brcm/brcmfmac43455-sdio.txt
var FirmwareBrcmBrcmfmac43455SdioTxt []byte

// FirmwareBrcmBrcmfmac43455SdioClmBlob returns the firmware/brcm/brcmfmac43455-sdio.clm_blob file.
//
//go:embed firmware/brcm/brcmfmac43455-sdio.clm_blob
var FirmwareBrcmBrcmfmac43455SdioClmBlob []byte

// FirmwareBrcmBrcmfmac43455SdioRaspberry returns the firmware/brcm/brcmfmac43455-sdio.Raspberry file.
//
//go:embed firmware/brcm/brcmfmac43455-sdio.Raspberry
var FirmwareBrcmBrcmfmac43455SdioRaspberry []byte

// ConfigTxt is the default configuration for the Raspberry Pi 4.
//
//go:embed config.txt
var ConfigTxt []byte

// Files is the mapping to the embedded iPXE binaries.
var Files = map[string][]byte{
	FirmwareFileName:               RpiEfi,
	"fixup4.dat":                   Fixup4Dat,
	"start4.elf":                   Start4ElfDat,
	"bcm2711-rpi-4-b.dtb":          Bcm2711Rpi4BDtb,
	"bcm2711-rpi-400.dtb":          Bcm2711Rpi400Dtb,
	"bcm2711-rpi-cm4.dtb":          Bcm2711RpiCm4Dtb,
	"miniuart-bt.dtbo":             OverlaysMiniUartBtDtbo,
	"upstream-pi4.dtbo":            OverlaysUpstreamPi4Dtbo,
	"rpi-poe-plus.dtbo":            OverlaysRpiPoePlusDtbo,
	"brcmfmac43455-sdio.bin":       FirmwareBrcmBrcmfmac43455SdioBin,
	"brcmfmac43455-sdio.txt":       FirmwareBrcmBrcmfmac43455SdioTxt,
	"brcmfmac43455-sdio.clm_blob":  FirmwareBrcmBrcmfmac43455SdioClmBlob,
	"brcmfmac43455-sdio.Raspberry": FirmwareBrcmBrcmfmac43455SdioRaspberry,
	"config.txt":                   ConfigTxt,
	"cmdline.txt":                  []byte(""),
	"bootcfg.txt":                  []byte(""),
}

func Read(macAddr net.HardwareAddr) ([]byte, error) {
	// Use cached varstore to avoid repeated parsing
	vs, err := varstore.New(RpiEfi)
	if err != nil {
		return nil, err
	}

	vl, err := vs.GetVarList()
	if err != nil {
		return nil, err
	}

	bootOption, err := efi.NewPxeBootOption(macAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create PXE boot option: %v", err)
	}

	bootNextTemplate := &efi.EfiVar{
		Name: efi.FromString("BootNext"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess,
		Data: []byte{0x99, 0x00},
	}

	// Set variables using pre-computed templates
	vl["Boot0099"] = bootOption
	vl["BootNext"] = bootNextTemplate

	return vs.ReadAll(vl)
}
