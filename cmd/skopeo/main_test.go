package main

import (
	"bytes"
	"testing"

	"github.com/containers/image/v5/types"
	"github.com/stretchr/testify/assert"
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
	res := opts.newSystemContext()
	assert.Equal(t, &types.SystemContext{
		// User-Agent is set by default.
		DockerRegistryUserAgent: defaultUserAgent,
	}, res)

	// Set everything to non-default values.
	opts, _ = fakeGlobalOptions(t, []string{
		"--registries.d", "/srv/registries.d",
		"--override-platform", "overridden-os/overridden-arch/overridden-variant",
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

	// Test legacy platform overrides.
	opts, _ = fakeGlobalOptions(t, []string{
		"--override-arch", "overridden-arch",
		"--override-os", "overridden-os",
		"--override-variant", "overridden-variant",
	})
	res = opts.newSystemContext()
	assert.Equal(t, &types.SystemContext{
		ArchitectureChoice:      "overridden-arch",
		OSChoice:                "overridden-os",
		VariantChoice:           "overridden-variant",
		DockerRegistryUserAgent: defaultUserAgent,
	}, res)
}
