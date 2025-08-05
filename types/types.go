// Package types contains common firmware related types and structures.
package types

// NetworkSettings contains network-related UEFI settings.
type NetworkSettings struct {
	MacAddress  string
	IPAddress   string
	SubnetMask  string
	Gateway     string
	DNSServers  []string
	EnableIPv6  bool
	EnableDHCP  bool
	VLANEnabled bool
	VLANID      string
}

// BootEntry represents a single UEFI boot entry.
type BootEntry struct {
	ID       string
	Name     string
	DevPath  string
	Enabled  bool
	OptData  string
	Position int
}

// SystemInfo contains firmware and system information.
type SystemInfo map[string]string
