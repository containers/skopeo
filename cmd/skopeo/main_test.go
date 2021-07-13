package main

import (
	"bytes"
	"testing"

	"github.com/containers/image/v5/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runSkopeo creates an app object and runs it with args, with an implied first "skopeo".
// Returns output intended for stdout and the returned error, if any.
func runSkopeo(args ...string) (string, error) {
	app, _ := createApp(false)
	stdout := bytes.Buffer{}
	app.SetOut(&stdout)
	app.SetArgs(args)
	err := app.Execute()
	return stdout.String(), err
}

func TestRootHelpFunc(t *testing.T) {
	for _, c := range []struct {
		subcommand string
		filter     bool
	}{
		{"", true},
		{"inspect", false},
		{"copy", false},
	} {
		var args []string
		if c.subcommand == "" {
			args = []string{"--help"}
		} else {
			args = []string{c.subcommand, "--help"}
		}

		unfilteredApp, _ := createApp(true)
		unfilteredStdout := bytes.Buffer{}
		unfilteredStderr := bytes.Buffer{}
		unfilteredApp.SetOut(&unfilteredStdout)
		unfilteredApp.SetErr(&unfilteredStderr)
		unfilteredApp.SetArgs(args)
		err := unfilteredApp.Execute()
		require.NoError(t, err, c.subcommand)

		normalApp, _ := createApp(true)
		normalStdout := bytes.Buffer{}
		normalStderr := bytes.Buffer{}
		normalApp.SetOut(&normalStdout)
		normalApp.SetErr(&normalStderr)
		normalApp.SetArgs(args)
		err = normalApp.Execute()
		require.NoError(t, err, c.subcommand)

		assert.Equal(t, unfilteredStderr, normalStderr, c.subcommand)
		if !c.filter {
			assert.Equal(t, unfilteredStdout, normalStdout, c.subcommand)
		} else {
			// FIXME
			assert.Equal(t, unfilteredStdout, normalStdout, c.subcommand)
		}
	}
}

func TestGlobalOptionsNewSystemContext(t *testing.T) {
	// Default state
	opts, _ := fakeGlobalOptions(t, []string{})
	res := opts.newSystemContext()
	assert.Equal(t, &types.SystemContext{
		// User-Agent is set by default.
		DockerRegistryUserAgent: defaultUserAgent,
	}, res)
	// Set everything to non-default values.
	opts, _ = fakeGlobalOptions(t, []string{
		"--registries.d", "/srv/registries.d",
		"--override-arch", "overridden-arch",
		"--override-os", "overridden-os",
		"--override-variant", "overridden-variant",
		"--tmpdir", "/srv",
		"--registries-conf", "/srv/registries.conf",
		"--tls-verify=false",
	})
	res = opts.newSystemContext()
	assert.Equal(t, &types.SystemContext{
		RegistriesDirPath:           "/srv/registries.d",
		ArchitectureChoice:          "overridden-arch",
		OSChoice:                    "overridden-os",
		VariantChoice:               "overridden-variant",
		BigFilesTemporaryDir:        "/srv",
		SystemRegistriesConfPath:    "/srv/registries.conf",
		DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
		DockerRegistryUserAgent:     defaultUserAgent,
	}, res)
}
