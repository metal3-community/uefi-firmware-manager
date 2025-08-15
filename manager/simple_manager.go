package manager

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"unsafe"

	"github.com/bmcpi/uefi-firmware-manager/edk2"
	"github.com/bmcpi/uefi-firmware-manager/efi"
	"github.com/bmcpi/uefi-firmware-manager/varstore"
	"github.com/go-logr/logr"
)

var (
	// Pre-decoded hex constant to avoid repeated parsing.
	pxeOptData = mustDecodeHex("4eac0881119f594d850ee21a522c59b2")
	
	// Pre-computed variable template for BootNext.
	bootNextTemplate = &efi.EfiVar{
		Name: efi.FromString("BootNext"),
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess,
		Data: []byte{0x99, 0x00},
	}
	
	// Pre-computed static parts for Boot0099 variable.
	boot0099Name = efi.FromString("Boot0099")
	
	// String builder pool for efficient string operations.
	stringBuilderPool = sync.Pool{
		New: func() any {
			sb := &strings.Builder{}
			sb.Grow(64) // Pre-allocate for typical MAC string length
			return sb
		},
	}
	
	// Varstore cache to avoid repeated parsing.
	varstoreCache struct {
		sync.RWMutex
		vs      *varstore.Edk2VarStore
		varList efi.EfiVarList
	}
	
	// MAC formatting lookup table for fast hex conversion.
	hexTable = "0123456789ABCDEF"
)

// SimpleFirmwareManager provides a memory-efficient way to create firmware with PXE boot variables.
type SimpleFirmwareManager struct {
	logger logr.Logger
}

// NewSimpleFirmwareManager creates a new SimpleFirmwareManager with minimal memory footprint.
func NewSimpleFirmwareManager(logger logr.Logger) (*SimpleFirmwareManager, error) {
	return &SimpleFirmwareManager{
		logger: logger,
	}, nil
}

// GetFirmwareReader returns an io.Reader for firmware with PXE variables, optimized for throughput.
func (sm *SimpleFirmwareManager) GetFirmwareReader(macAddr net.HardwareAddr) (io.Reader, error) {
	// Use cached varstore to avoid repeated parsing
	vs, varList, err := sm.getOrCreateVarstore()
	if err != nil {
		return nil, fmt.Errorf("failed to get varstore: %v", err)
	}

	// Clone the variable list for this request (shallow copy)
	requestVarList := make(efi.EfiVarList, len(varList))
	for k, v := range varList {
		requestVarList[k] = v
	}

	// Create device path and boot entry efficiently
	devPath := (&efi.DevicePath{}).Mac(macAddr).IPv4()
	
	// Fast MAC address formatting using optimized conversion
	title := efi.NewUCS16String(formatMACTitle(macAddr))

	// Create boot entry with pre-allocated data
	bootEntry := &efi.BootEntry{
		Attr:       efi.LOAD_OPTION_ACTIVE,
		Title:      *title,
		DevicePath: *devPath,
		OptData:    pxeOptData, // Use pre-decoded constant
	}

	// Set variables using pre-computed templates
	requestVarList["Boot0099"] = &efi.EfiVar{
		Name: boot0099Name,
		Guid: efi.EFI_GLOBAL_VARIABLE_GUID,
		Attr: efi.EfiVariableDefault | efi.EfiVariableRuntimeAccess,
		Data: bootEntry.Bytes(),
	}

	requestVarList["BootNext"] = bootNextTemplate

	// Return streaming reader directly - no intermediate storage
	return vs.ReadBytes(requestVarList)
}

// GetBaseReader returns a reader for the base firmware without modifications.
func (sm *SimpleFirmwareManager) GetBaseReader() io.Reader {
	// Return optimized reader with ReadSeeker interface
	return &optimizedFirmwareReader{
		data: edk2.RpiEfi,
		size: int64(len(edk2.RpiEfi)),
	}
}

// GetBaseReadSeeker returns a ReadSeeker for the base firmware (useful for HTTP Range requests).
func (sm *SimpleFirmwareManager) GetBaseReadSeeker() io.ReadSeeker {
	return &optimizedFirmwareReader{
		data: edk2.RpiEfi,
		size: int64(len(edk2.RpiEfi)),
	}
}

// Size returns the size of the base firmware data.
func (sm *SimpleFirmwareManager) Size() int64 {
	return int64(len(edk2.RpiEfi))
}

// getOrCreateVarstore gets cached varstore or creates new one with caching.
func (sm *SimpleFirmwareManager) getOrCreateVarstore() (*varstore.Edk2VarStore, efi.EfiVarList, error) {
	// Try to get from cache first (read lock)
	varstoreCache.RLock()
	if varstoreCache.vs != nil && varstoreCache.varList != nil {
		vs := varstoreCache.vs
		varList := varstoreCache.varList
		varstoreCache.RUnlock()
		return vs, varList, nil
	}
	varstoreCache.RUnlock()

	// Create new varstore (write lock)
	varstoreCache.Lock()
	defer varstoreCache.Unlock()
	
	// Double-check pattern
	if varstoreCache.vs != nil && varstoreCache.varList != nil {
		return varstoreCache.vs, varstoreCache.varList, nil
	}

	vs, err := varstore.New(edk2.RpiEfi)
	if err != nil {
		return nil, nil, err
	}
	vs.Logger = sm.logger

	varList, err := vs.GetVarList()
	if err != nil {
		return nil, nil, err
	}

	// Cache for future use
	varstoreCache.vs = vs
	varstoreCache.varList = varList

	return vs, varList, nil
}

// formatMACTitle creates MAC title string with optimized formatting.
func formatMACTitle(macAddr net.HardwareAddr) string {
	if len(macAddr) != 6 {
		// Fallback for non-standard MAC addresses
		return fmt.Sprintf("UEFI PXEv4 (MAC:%s)", strings.ToUpper(macAddr.String()))
	}

	// Use string builder pool for efficient formatting
	sb := stringBuilderPool.Get().(*strings.Builder)
	defer func() {
		sb.Reset()
		stringBuilderPool.Put(sb)
	}()

	// Pre-allocate exact size: "UEFI PXEv4 (MAC:" + "XX:XX:XX:XX:XX:XX" + ")"
	sb.Grow(32)
	
	sb.WriteString("UEFI PXEv4 (MAC:")
	
	// Direct byte-to-hex conversion for maximum speed
	for i, b := range macAddr {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteByte(hexTable[b>>4])
		sb.WriteByte(hexTable[b&0x0F])
	}
	
	sb.WriteByte(')')
	return sb.String()
}

// optimizedFirmwareReader provides a zero-copy reader with ReadSeeker interface.
type optimizedFirmwareReader struct {
	data []byte
	pos  int64
	size int64
}

func (fr *optimizedFirmwareReader) Read(p []byte) (n int, err error) {
	if fr.pos >= fr.size {
		return 0, io.EOF
	}
	
	available := fr.size - fr.pos
	if int64(len(p)) > available {
		p = p[:available]
	}
	
	// Use unsafe pointer arithmetic for maximum speed
	n = copy(p, (*[1<<30]byte)(unsafe.Pointer(&fr.data[fr.pos]))[:len(p):len(p)])
	fr.pos += int64(n)
	return n, nil
}

func (fr *optimizedFirmwareReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = fr.pos + offset
	case io.SeekEnd:
		newPos = fr.size + offset
	default:
		return 0, fmt.Errorf("invalid whence value: %d", whence)
	}
	
	if newPos < 0 {
		return 0, fmt.Errorf("negative position: %d", newPos)
	}
	
	if newPos > fr.size {
		newPos = fr.size
	}
	
	fr.pos = newPos
	return newPos, nil
}

// mustDecodeHex decodes a hex string and panics on error.
func mustDecodeHex(s string) []byte {
	data, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("failed to decode hex string %q: %v", s, err))
	}
	return data
}
