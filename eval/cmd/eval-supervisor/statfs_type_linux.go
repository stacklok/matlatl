//go:build linux

package main

func statfsType(value any) int64 {
	typeNumber, ok := value.(int64)
	if !ok {
		return -1
	}
	return typeNumber
}
