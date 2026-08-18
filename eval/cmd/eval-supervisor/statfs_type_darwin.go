//go:build darwin

package main

func statfsType(value any) int64 {
	typeNumber, ok := value.(uint32)
	if !ok {
		return -1
	}
	return int64(typeNumber)
}
