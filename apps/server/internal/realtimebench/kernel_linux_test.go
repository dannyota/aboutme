//go:build linux

package realtimebench_test

import "syscall"

func kernelRelease() string {
	var value syscall.Utsname
	if err := syscall.Uname(&value); err != nil {
		return "unknown"
	}
	return charsToString(value.Release[:])
}

func charsToString(value []int8) string {
	bytes := make([]byte, 0, len(value))
	for _, character := range value {
		if character == 0 {
			break
		}
		bytes = append(bytes, byte(character))
	}
	return string(bytes)
}
