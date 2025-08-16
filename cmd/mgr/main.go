package main

import (
	"io"
	"net"
	"os"

	"github.com/go-logr/logr"
	"github.com/metal3-community/uefi-firmware-manager/manager"
)

func main() {
	log := logr.Logger.WithName(logr.Logger{}, "main")
	mgr, err := manager.NewSimpleFirmwareManager(log)
	if err != nil {
		log.Error(err, "failed to create firmware manager")
	}
	mac, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		log.Error(err, "failed to parse MAC address")
	}

	reader, err := mgr.GetFirmwareReader(mac)
	if err != nil {
		log.Error(err, "failed to get firmware reader")
	}
	file, err := os.OpenFile("RPI_EFI.fd", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		log.Error(err, "failed to create firmware file")
	}
	defer file.Close()
	_, err = io.Copy(file, reader)
	if err != nil {
		log.Error(err, "failed to write firmware file")
	}
}
