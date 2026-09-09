//go:build windows

package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type proxyOptions struct {
	global *globalOptions
}

func proxyCmd(global *globalOptions) *cobra.Command {
	opts := proxyOptions{global: global}
	cmd := &cobra.Command{
		RunE: commandAction(opts.run),
		Args: cobra.ExactArgs(0),
		// Not stabilized yet
		Hidden: true,
	}
	return cmd
}

func (opts *proxyOptions) run(args []string, stdout io.Writer) error {
	return fmt.Errorf("this command is not supported on Windows")
}
