//go:build !linux

package system

// Non-Linux builds support generation/testing; the CLI refuses system deployment.
func lock(string) (func(), error)     { return func() {}, nil }
func readLock(string) (func(), error) { return func() {}, nil }
