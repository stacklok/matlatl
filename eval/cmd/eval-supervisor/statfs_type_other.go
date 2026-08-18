//go:build !linux && !darwin

package main

func statfsType(any) int64 { return -1 }
