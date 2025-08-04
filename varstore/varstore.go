package varstore

import "github.com/bmcpi/uefi-firmware-manager/efi"

type VarStore interface {
	GetVarList() (efi.EfiVarList, error)
	WriteVarStore(filename string, varlist efi.EfiVarList) error
}
