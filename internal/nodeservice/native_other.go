//go:build !darwin && !linux && !windows

package nodeservice

import "errors"

func nativeRuntime() (runtimeInfo, error) {
	return runtimeInfo{}, errors.New("unsupported user-service platform")
}
