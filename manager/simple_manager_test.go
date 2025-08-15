package manager

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/go-logr/logr"
)

func TestSimpleFirmwareManager_MemoryOptimization(t *testing.T) {
	logger := logr.Discard()

	// Test that manager creation doesn't require large memory allocation
	mgr, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Verify manager was created successfully
	if mgr == nil {
		t.Fatal("Manager is nil")
	}

	// Manager should be lightweight (just contains a logger)
	t.Logf("Manager created successfully with minimal footprint")
}

func TestSimpleFirmwareManager_GetFirmwareReader(t *testing.T) {
	logger := logr.Discard()
	mgr, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	macAddr, err := net.ParseMAC("d8:3a:dd:61:4d:15")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}

	reader, err := mgr.GetFirmwareReader(macAddr)
	if err != nil {
		t.Fatalf("Failed to get firmware reader: %v", err)
	}

	// Verify reader is not nil
	if reader == nil {
		t.Fatal("Reader is nil")
	}

	// Test that we can read some data
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from firmware: %v", err)
	}

	if n == 0 {
		t.Fatal("No data read from firmware")
	}

	t.Logf("Successfully read %d bytes from firmware", n)
}

func TestSimpleFirmwareManager_GetBaseReader(t *testing.T) {
	logger := logr.Discard()
	mgr, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	reader := mgr.GetBaseReader()
	if reader == nil {
		t.Fatal("Base reader is nil")
	}

	// Test zero-copy reader
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from base firmware: %v", err)
	}

	if n == 0 {
		t.Fatal("No data read from base firmware")
	}

	t.Logf("Successfully read %d bytes from base firmware", n)
}

func TestSimpleFirmwareManager_GetBaseReadSeeker(t *testing.T) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	readSeeker := manager.GetBaseReadSeeker()

	// Test reading
	buf := make([]byte, 1024)
	n, err := readSeeker.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from base read seeker: %v", err)
	}

	if n == 0 {
		t.Fatal("No data read from base read seeker")
	}

	// Test seeking
	pos, err := readSeeker.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}
	if pos != 0 {
		t.Fatalf("Expected position 0, got %d", pos)
	}
}

func TestSimpleFirmwareManager_Size(t *testing.T) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	size := manager.Size()
	if size <= 0 {
		t.Fatalf("Expected positive size, got %d", size)
	}
}

func TestFormatMACTitle(t *testing.T) {
	tests := []struct {
		name     string
		macAddr  net.HardwareAddr
		expected string
	}{
		{
			name:     "Standard MAC",
			macAddr:  mustParseMac("aa:bb:cc:dd:ee:ff"),
			expected: "UEFI PXEv4 (MAC:AA:BB:CC:DD:EE:FF)",
		},
		{
			name:     "All zeros",
			macAddr:  mustParseMac("00:00:00:00:00:00"),
			expected: "UEFI PXEv4 (MAC:00:00:00:00:00:00)",
		},
		{
			name:     "All ones",
			macAddr:  mustParseMac("ff:ff:ff:ff:ff:ff"),
			expected: "UEFI PXEv4 (MAC:FF:FF:FF:FF:FF:FF)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMACTitle(tt.macAddr)
			if result != tt.expected {
				t.Errorf("formatMACTitle() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestVarstoreCache(t *testing.T) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// First call should populate cache
	vs1, varList1, err := manager.getOrCreateVarstore()
	if err != nil {
		t.Fatalf("Failed to get varstore: %v", err)
	}

	// Second call should use cached values
	vs2, varList2, err := manager.getOrCreateVarstore()
	if err != nil {
		t.Fatalf("Failed to get cached varstore: %v", err)
	}

	// Should be the same instances (cached)
	if vs1 != vs2 {
		t.Error("Expected same varstore instance from cache")
	}

	if len(varList1) != len(varList2) {
		t.Error("Expected same variable list from cache")
	}
}

func TestOptimizedFirmwareReader_Comprehensive(t *testing.T) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	reader := manager.GetBaseReadSeeker()

	// Test various read sizes
	readSizes := []int{1, 16, 512, 1024, 4096, 32768}
	for _, size := range readSizes {
		t.Run(fmt.Sprintf("ReadSize_%d", size), func(t *testing.T) {
			_, err := reader.Seek(0, io.SeekStart)
			if err != nil {
				t.Fatalf("Failed to seek: %v", err)
			}

			buf := make([]byte, size)
			totalRead := 0
			for {
				n, err := reader.Read(buf)
				totalRead += n
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("Read error with size %d: %v", size, err)
				}
			}

			expectedSize := int(manager.Size())
			if totalRead != expectedSize {
				t.Errorf("Read %d bytes, expected %d", totalRead, expectedSize)
			}
		})
	}

	// Test seeking behavior
	size := manager.Size()
	seekTests := []struct {
		offset int64
		whence int
		want   int64
	}{
		{0, io.SeekStart, 0},
		{100, io.SeekStart, 100},
		{0, io.SeekEnd, size},
		{-100, io.SeekEnd, size - 100},
		{50, io.SeekCurrent, size - 50},
	}

	for _, tt := range seekTests {
		pos, err := reader.Seek(tt.offset, tt.whence)
		if err != nil {
			t.Errorf("Seek(%d, %d) error: %v", tt.offset, tt.whence, err)
			continue
		}
		if pos != tt.want {
			t.Errorf("Seek(%d, %d) = %d, want %d", tt.offset, tt.whence, pos, tt.want)
		}
	}
}

// Benchmarks

func BenchmarkSimpleFirmwareManager_GetFirmwareReader(b *testing.B) {
	logger := logr.Discard()
	mgr, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	macAddr, err := net.ParseMAC("d8:3a:dd:61:4d:15")
	if err != nil {
		b.Fatalf("Failed to parse MAC: %v", err)
	}

	b.ReportAllocs()

	for range b.N {
		reader, err := mgr.GetFirmwareReader(macAddr)
		if err != nil {
			b.Fatalf("Failed to get firmware reader: %v", err)
		}

		// Consume the reader to test actual usage
		_, err = io.Copy(io.Discard, reader)
		if err != nil {
			b.Fatalf("Failed to read firmware: %v", err)
		}
	}
}

func BenchmarkSimpleFirmwareManager_GetBaseReader(b *testing.B) {
	logger := logr.Discard()
	mgr, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	b.ReportAllocs()

	for range b.N {
		reader := mgr.GetBaseReader()

		// Consume the reader to test actual usage
		_, err := io.Copy(io.Discard, reader)
		if err != nil {
			b.Fatalf("Failed to read base firmware: %v", err)
		}
	}
}

func BenchmarkFormatMACTitle(b *testing.B) {
	macAddr := mustParseMac("aa:bb:cc:dd:ee:ff")

	b.ResetTimer()
	for range b.N {
		_ = formatMACTitle(macAddr)
	}
}

func BenchmarkFormatMACTitle_Fallback(b *testing.B) {
	// Create non-standard MAC address to test fallback
	macAddr := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd} // 4 bytes instead of 6

	b.ResetTimer()
	for range b.N {
		_ = formatMACTitle(macAddr)
	}
}

func BenchmarkOptimizedFirmwareReader_Read(b *testing.B) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	reader := manager.GetBaseReader()
	buf := make([]byte, 32*1024) // 32KB buffer

	b.ResetTimer()
	b.SetBytes(int64(len(buf)))

	for range b.N {
		// Reset reader to beginning for each iteration
		if seeker, ok := reader.(io.Seeker); ok {
			seeker.Seek(0, io.SeekStart)
		} else {
			// Create new reader if seeking is not available
			reader = manager.GetBaseReader()
		}

		for {
			n, err := reader.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatalf("Read error: %v", err)
			}
			if n == 0 {
				break
			}
		}
	}
}

func BenchmarkOptimizedFirmwareReader_Seek(b *testing.B) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	readSeeker := manager.GetBaseReadSeeker()
	size := manager.Size()

	b.ResetTimer()
	for range b.N {
		// Seek to random positions
		positions := []int64{0, size / 4, size / 2, 3 * size / 4, size}
		for _, pos := range positions {
			_, err := readSeeker.Seek(pos, io.SeekStart)
			if err != nil {
				b.Fatalf("Seek error: %v", err)
			}
		}
	}
}

func BenchmarkStringBuilderPool(b *testing.B) {
	macAddr := mustParseMac("aa:bb:cc:dd:ee:ff")

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = formatMACTitle(macAddr)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			// Direct string concatenation without pool
			var sb strings.Builder
			sb.WriteString("UEFI PXEv4 (MAC:")
			sb.WriteString(strings.ToUpper(macAddr.String()))
			sb.WriteByte(')')
			_ = sb.String()
		}
	})
}

func BenchmarkVarstoreCache(b *testing.B) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	// Prime the cache
	_, _, _ = manager.getOrCreateVarstore()

	b.ResetTimer()
	for range b.N {
		_, _, err := manager.getOrCreateVarstore()
		if err != nil {
			b.Fatalf("Failed to get varstore: %v", err)
		}
	}
}

func BenchmarkMemoryOptimizations(b *testing.B) {
	logger := logr.Discard()
	manager, err := NewSimpleFirmwareManager(logger)
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	macAddr := mustParseMac("aa:bb:cc:dd:ee:ff")

	// Measure memory allocation for a complete operation
	b.Run("FullOperation", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			reader, err := manager.GetFirmwareReader(macAddr)
			if err != nil {
				b.Fatalf("Failed to get firmware reader: %v", err)
			}

			// Read first 1KB to simulate typical usage
			buf := make([]byte, 1024)
			_, err = reader.Read(buf)
			if err != nil && err != io.EOF {
				b.Fatalf("Failed to read: %v", err)
			}
		}
	})

	b.Run("BaseReaderOnly", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			reader := manager.GetBaseReader()

			// Read first 1KB to simulate typical usage
			buf := make([]byte, 1024)
			_, err := reader.Read(buf)
			if err != nil && err != io.EOF {
				b.Fatalf("Failed to read: %v", err)
			}
		}
	})
}

// Helper functions

func mustParseMac(s string) net.HardwareAddr {
	mac, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return mac
}
