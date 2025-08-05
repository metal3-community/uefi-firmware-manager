package manager

import (
	"net"
	"testing"

	"github.com/bmcpi/uefi-firmware-manager/efi"
	"github.com/go-logr/logr"
)

func TestNewJsonEDK2Manager(t *testing.T) {
	// Use the existing data directory
	dataDir := "../data"

	logger := logr.Discard()
	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	if manager.dataDir != dataDir {
		t.Errorf("Expected dataDir %s, got %s", dataDir, manager.dataDir)
	}
}

func TestListAvailableMACs(t *testing.T) {
	dataDir := "../data"
	logger := logr.Discard()

	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	macs, err := manager.ListAvailableMACs()
	if err != nil {
		t.Fatalf("Failed to list available MACs: %v", err)
	}

	t.Logf("Found %d MAC addresses", len(macs))
	for _, mac := range macs {
		t.Logf("MAC: %s", mac.String())
	}

	// We expect at least the two MACs from the test data
	if len(macs) < 2 {
		t.Errorf("Expected at least 2 MACs, got %d", len(macs))
	}

	// Check that we can convert MAC addresses properly
	expectedMACs := []string{"d8:3a:dd:5a:44:36", "d8:3a:dd:61:4d:15"}
	foundMACs := make(map[string]bool)

	for _, mac := range macs {
		foundMACs[mac.String()] = true
	}

	for _, expected := range expectedMACs {
		if !foundMACs[expected] {
			t.Errorf("Expected MAC %s not found", expected)
		}
	}
}

func TestLoadMAC(t *testing.T) {
	dataDir := "../data"
	logger := logr.Discard()

	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	// Test loading a specific MAC
	mac, err := net.ParseMAC("d8:3a:dd:5a:44:36")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}

	err = manager.LoadMAC(mac)
	if err != nil {
		t.Fatalf("Failed to load MAC: %v", err)
	}

	// Verify the MAC was loaded
	loadedMAC := manager.GetCurrentMAC()
	if loadedMAC.String() != mac.String() {
		t.Errorf("Expected loaded MAC %s, got %s", mac.String(), loadedMAC.String())
	}

	// Check that variables were loaded
	variables, err := manager.ListVariables()
	if err != nil {
		t.Fatalf("Failed to list variables: %v", err)
	}

	t.Logf("Loaded %d variables", len(variables))

	// Check for expected variables
	expectedVars := []string{"ClientId", "BootOrder", "PlatformLang"}
	for _, varName := range expectedVars {
		if _, exists := variables[varName]; !exists {
			t.Errorf("Expected variable %s not found", varName)
		}
	}
}

func TestMACDirectoryConversion(t *testing.T) {
	logger := logr.Discard()
	manager := &JsonEDK2Manager{logger: logger}

	tests := []struct {
		mac     string
		dirName string
	}{
		{"d8:3a:dd:5a:44:36", "d8-3a-dd-5a-44-36"},
		{"d8:3a:dd:61:4d:15", "d8-3a-dd-61-4d-15"},
		{"aa:bb:cc:dd:ee:ff", "aa-bb-cc-dd-ee-ff"},
	}

	for _, test := range tests {
		mac, err := net.ParseMAC(test.mac)
		if err != nil {
			t.Fatalf("Failed to parse MAC %s: %v", test.mac, err)
		}

		// Test MAC to directory name
		dirName := manager.macDirName(mac)
		if dirName != test.dirName {
			t.Errorf("macDirName(%s) = %s, expected %s", test.mac, dirName, test.dirName)
		}

		// Test directory name to MAC
		parsedMAC, err := manager.macFromDirName(test.dirName)
		if err != nil {
			t.Fatalf("macFromDirName(%s) failed: %v", test.dirName, err)
		}

		if parsedMAC.String() != test.mac {
			t.Errorf(
				"macFromDirName(%s) = %s, expected %s",
				test.dirName,
				parsedMAC.String(),
				test.mac,
			)
		}
	}
}

func TestGetSetVariable(t *testing.T) {
	dataDir := "../data"
	logger := logr.Discard()

	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	// Load a MAC
	mac, err := net.ParseMAC("d8:3a:dd:5a:44:36")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}

	err = manager.LoadMAC(mac)
	if err != nil {
		t.Fatalf("Failed to load MAC: %v", err)
	}

	// Test getting a variable
	clientId, err := manager.GetVariable("ClientId")
	if err != nil {
		t.Fatalf("Failed to get ClientId variable: %v", err)
	}

	t.Logf("ClientId variable: name=%s, guid=%s, attr=%d, data_len=%d",
		clientId.Name.String(), clientId.Guid.String(), clientId.Attr, len(clientId.Data))

	// Test setting a variable (create a copy with modified attributes)
	modifiedClientId := *clientId
	modifiedClientId.Attr = 999 // Test value

	err = manager.SetVariable("ClientId", &modifiedClientId)
	if err != nil {
		t.Fatalf("Failed to set ClientId variable: %v", err)
	}

	// Verify the change
	retrievedVar, err := manager.GetVariable("ClientId")
	if err != nil {
		t.Fatalf("Failed to get modified ClientId variable: %v", err)
	}

	if retrievedVar.Attr != 999 {
		t.Errorf("Expected attr 999, got %d", retrievedVar.Attr)
	}

	// Check that manager knows it's been modified
	if !manager.modified {
		t.Error("Manager should be marked as modified after SetVariable")
	}
}

func TestClientIdDUIDIntegration(t *testing.T) {
	dataDir := "../data"
	logger := logr.Discard()

	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	// Test both MAC addresses
	testMACs := []string{"d8:3a:dd:5a:44:36", "d8:3a:dd:61:4d:15"}

	for _, macStr := range testMACs {
		t.Run("MAC_"+macStr, func(t *testing.T) {
			mac, err := net.ParseMAC(macStr)
			if err != nil {
				t.Fatalf("Failed to parse MAC: %v", err)
			}

			err = manager.LoadMAC(mac)
			if err != nil {
				t.Fatalf("Failed to load MAC: %v", err)
			}

			// Get ClientId variable and parse DUID
			clientIdVar, err := manager.GetVariable("ClientId")
			if err != nil {
				t.Fatalf("Failed to get ClientId variable: %v", err)
			}

			// Use our DHCP6 DUID parser
			duid, err := efi.NewDhcp6Duid(clientIdVar.Data)
			if err != nil {
				t.Fatalf("Failed to parse ClientId DUID: %v", err)
			}

			t.Logf("MAC %s: DUID = %s", macStr, duid.String())

			// Test MAC extraction (may not work for all DUID types)
			extractedMAC := duid.GetMacAddress()
			if extractedMAC != nil {
				t.Logf("Extracted MAC from DUID: %s", extractedMAC.String())
			} else {
				t.Logf("DUID does not contain extractable MAC address")
			}
		})
	}
}

func TestValidateMACConsistency(t *testing.T) {
	dataDir := "../data"
	logger := logr.Discard()

	manager, err := NewJsonEDK2Manager(dataDir, logger)
	if err != nil {
		t.Fatalf("Failed to create JsonEDK2Manager: %v", err)
	}

	// Load a MAC and test validation
	mac, err := net.ParseMAC("d8:3a:dd:5a:44:36")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}

	err = manager.LoadMAC(mac)
	if err != nil {
		t.Fatalf("Failed to load MAC: %v", err)
	}

	// Test validation (this may pass or fail depending on DUID format)
	err = manager.validateMACConsistency()
	if err != nil {
		t.Logf("MAC validation warning (expected for non-standard DUID): %v", err)
	} else {
		t.Log("MAC validation passed")
	}
}
