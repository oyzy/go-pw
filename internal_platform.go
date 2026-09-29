package main

import "runtime"

// goos is a tiny indirection that keeps main.go free of platform switches in tests.
func goos() string { return runtime.GOOS }
