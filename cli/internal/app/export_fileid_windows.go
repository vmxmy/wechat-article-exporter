//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func exportRootIdentityFromFile(file *os.File) (uint64, uint64, error) {
	if file == nil {
		return 0, 0, errors.New("filesystem handle is unavailable")
	}
	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &data); err != nil {
		return 0, 0, err
	}
	return uint64(data.VolumeSerialNumber), uint64(data.FileIndexHigh)<<32 | uint64(data.FileIndexLow), nil
}
