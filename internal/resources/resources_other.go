//go:build !darwin

package resources

import "errors"

func nativeReader() (func() (reading, error), error) {
	return nil, errors.New("System monitoring is currently available on macOS")
}
