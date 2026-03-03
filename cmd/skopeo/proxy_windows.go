//go:build windows

package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func proxyCmd(global *globalOptions) *cobra.Command {
	return &cobra.Command{
		RunE: commandAction(func(args []string, stdout io.Writer) error {
			return fmt.Errorf("this command is not supported on Windows")
		}),
		Args:   cobra.ExactArgs(0),
		Hidden: true,
	}
}
