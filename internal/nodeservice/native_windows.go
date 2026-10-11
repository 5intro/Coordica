//go:build windows

package nodeservice

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func nativeRuntime() (runtimeInfo, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return runtimeInfo{}, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return runtimeInfo{}, err
	}
	sid, err := user.User.Sid.String()
	if err != nil {
		return runtimeInfo{}, err
	}
	buffer := make([]uint16, 32768)
	size, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if size == 0 || size >= uintptr(len(buffer)) {
		return runtimeInfo{}, errors.New("Windows system directory is unavailable")
	}
	manager := filepath.Join(syscall.UTF16ToString(buffer[:size]), "schtasks.exe")
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimeInfo{}, err
	}
	return runtimeInfo{Platform: "windows", Home: home, SID: sid, Path: os.Getenv("PATH"), Manager: manager}, nil
}
