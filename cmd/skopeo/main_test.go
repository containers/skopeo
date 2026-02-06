package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
)

// runSkopeo creates an app object and runs it with args, with an implied first "skopeo".
// Returns output intended for stdout and the returned error, if any.
func runSkopeo(args ...string) (string, error) {
	app, _ := createApp()
	stdout := bytes.Buffer{}
	app.SetOut(&stdout)
	app.SetArgs(args)
	err := app.Execute()
	return stdout.String(), err
}

func TestGlobalOptionsNewSystemContext(t *testing.T) {
	// Default state
	opts, _ := fakeGlobalOptions(t, []string{})
	res, err := opts.newSystemContext()
	require.NoError(t, err)
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
	res, err = opts.newSystemContext()
	require.NoError(t, err)
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
