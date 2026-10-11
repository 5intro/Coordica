//go:build darwin

package nodeservice

import (
	"os"
	"strconv"
)

func nativeRuntime() (runtimeInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimeInfo{}, err
	}
	return runtimeInfo{Platform: "darwin", Home: home, UID: strconv.Itoa(os.Getuid()), Path: os.Getenv("PATH"), Manager: "/bin/launchctl"}, nil
}
