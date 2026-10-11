//go:build linux

package nodeservice

import (
	"errors"
	"os"
)

func nativeRuntime() (runtimeInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimeInfo{}, err
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return runtimeInfo{}, err
	}
	for _, path := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return runtimeInfo{Platform: "linux", Home: home, ConfigHome: configHome, Path: os.Getenv("PATH"), Manager: path}, nil
		}
	}
	return runtimeInfo{}, errors.New("systemd user manager is not installed")
}
