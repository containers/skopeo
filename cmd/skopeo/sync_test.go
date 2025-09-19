package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
	"gopkg.in/yaml.v3"
)

var _ yaml.Unmarshaler = (*tlsVerifyConfig)(nil)

func TestTLSVerifyConfig(t *testing.T) {
	type container struct { // An example of a larger config file
		TLSVerify tlsVerifyConfig `yaml:"tls-verify"`
	}

	for _, c := range []struct {
		input    string
		expected tlsVerifyConfig
	}{
		{
			input:    `tls-verify: true`,
			expected: tlsVerifyConfig{skip: types.OptionalBoolFalse},
		},
		{
			input:    `tls-verify: false`,
			expected: tlsVerifyConfig{skip: types.OptionalBoolTrue},
		},
		{
			input:    ``, // No value
			expected: tlsVerifyConfig{skip: types.OptionalBoolUndefined},
		},
	} {
		config := container{}
		err := yaml.Unmarshal([]byte(c.input), &config)
		require.NoError(t, err, c.input)
		assert.Equal(t, c.expected, config.TLSVerify, c.input)
	}

	// Invalid input
	config := container{}
	err := yaml.Unmarshal([]byte(`tls-verify: "not a valid bool"`), &config)
	assert.Error(t, err)
}

func TestSync(t *testing.T) {
	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1"},
		{"a1", "a2", "a3"},
	} {
		out, err := runSkopeo(append([]string{"sync"}, args...)...)
		assertTestFailed(t, out, err, "Exactly two arguments expected")
	}

	// FIXME: Much more test coverage
	// Actual feature tests exist in integration and systemtest
}

// TestSyncTLSPrecedence validates the interactions of tls-verify in YAML and --src-tls-verify in the CLI.
func TestSyncTLSPrecedence(t *testing.T) {
	for _, tt := range []struct {
		cli            string
		yaml           string
		wantSkip       types.OptionalBool
		wantDaemonSkip bool
	}{
		{"--src-tls-verify=false", `# nothing`, types.OptionalBoolTrue, true},
		{"--src-tls-verify=true", `# nothing`, types.OptionalBoolFalse, false},
		{"", `# nothing`, types.OptionalBoolUndefined, false},
		{"--src-tls-verify=false", "tls-verify: true", types.OptionalBoolFalse, false},
		{"--src-tls-verify=true", "tls-verify: false", types.OptionalBoolTrue, true},
	} {
		t.Run(fmt.Sprintf("%#v + %q", tt.cli, tt.yaml), func(t *testing.T) {
			opts := fakeImageOptions(t, "src-", true, []string{}, []string{tt.cli})
			sourceCtx, err := opts.newSystemContext()
			require.NoError(t, err)
			var cfg registrySyncConfig
			err = yaml.Unmarshal(fmt.Appendf(nil, `
%s
images:
  repo: # Specifying an explicit repo+tag avoids imagesToCopyFromRegistry trying to contact the registry.
    - latest
`, tt.yaml,
			), &cfg)
			require.NoError(t, err)

			descs, err := imagesToCopyFromRegistry("example.com", cfg, *sourceCtx)
			require.NoError(t, err)
			require.NotEmpty(t, descs)
			ctx := descs[0].Context
			require.NotNil(t, ctx)
			assert.Equal(t, tt.wantSkip, ctx.DockerInsecureSkipTLSVerify)
			assert.Equal(t, tt.wantDaemonSkip, ctx.DockerDaemonInsecureSkipTLSVerify)
		})
	}
}
