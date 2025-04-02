//go:build !linux

package main

func maybeReexec() error {
	return nil
}

func reexecIfNecessaryForImages(_ ...string) error {
	return nil
}
