package types_test

import (
	"testing"

	"github.com/metal3-community/uefi-firmware-manager/types"
	"github.com/stretchr/testify/assert"
)

func TestSystemInfo(t *testing.T) {
	// Test basic map operations
	info := types.SystemInfo{}

	// Test setting and getting values
	info["FirmwareVersion"] = "1.0.0"
	info["PlatformName"] = "RaspberryPi"

	assert.Equal(t, "1.0.0", info["FirmwareVersion"])
	assert.Equal(t, "RaspberryPi", info["PlatformName"])

	// Test map length
	assert.Len(t, info, 2)

	// Test deleting an entry
	delete(info, "FirmwareVersion")
	assert.Len(t, info, 1)
	assert.Empty(t, info["FirmwareVersion"])
}

func TestNetworkSettings(t *testing.T) {
	settings := types.NetworkSettings{
		MacAddress:  "01:02:03:04:05:06",
		IPAddress:   "192.168.1.100",
		SubnetMask:  "255.255.255.0",
		Gateway:     "192.168.1.1",
		DNSServers:  []string{"8.8.8.8", "8.8.4.4"},
		EnableIPv6:  true,
		EnableDHCP:  false,
		VLANEnabled: true,
		VLANID:      "100",
	}

	assert.Equal(t, "01:02:03:04:05:06", settings.MacAddress)
	assert.Equal(t, "192.168.1.100", settings.IPAddress)
	assert.Equal(t, "255.255.255.0", settings.SubnetMask)
	assert.Equal(t, "192.168.1.1", settings.Gateway)
	assert.Equal(t, []string{"8.8.8.8", "8.8.4.4"}, settings.DNSServers)
	assert.True(t, settings.EnableIPv6)
	assert.False(t, settings.EnableDHCP)
	assert.True(t, settings.VLANEnabled)
	assert.Equal(t, "100", settings.VLANID)
}

func TestBootEntry(t *testing.T) {
	entry := types.BootEntry{
		ID:       "0001",
		Name:     "UEFI Network Boot",
		DevPath:  "PciRoot(0)/Pci(2,0)/MAC()/IPv4()/Pxe()",
		Enabled:  true,
		OptData:  "0102030405",
		Position: 0,
	}

	assert.Equal(t, "0001", entry.ID)
	assert.Equal(t, "UEFI Network Boot", entry.Name)
	assert.Equal(t, "PciRoot(0)/Pci(2,0)/MAC()/IPv4()/Pxe()", entry.DevPath)
	assert.True(t, entry.Enabled)
	assert.Equal(t, "0102030405", entry.OptData)
	assert.Equal(t, 0, entry.Position)
}
