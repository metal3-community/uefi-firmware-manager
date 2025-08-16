package varstore

import "github.com/metal3-community/uefi-firmware-manager/efi"

type VarStore interface {
	GetVarList() (efi.EfiVarList, error)
	WriteVarStore(filename string, varlist efi.EfiVarList) error
}
