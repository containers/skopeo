package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newExpectedSystemContext creates a SystemContext with expected defaults for tests,
// accounting for platform-specific behavior like macOS Docker socket location
func newExpectedSystemContext() *types.SystemContext {
	ctx := &types.SystemContext{
		DockerRegistryUserAgent: defaultUserAgent,
	}
	// On macOS, DockerDaemonHost gets set to the platform-specific default
	if runtime.GOOS == "darwin" {
		if home := os.Getenv("HOME"); home != "" {
			ctx.DockerDaemonHost = "unix://" + filepath.Join(home, ".docker/run/docker.sock")
		}
	}
	return ctx
}

func TestNoteCloseFailure(t *testing.T) {
	const description = "description"

	mainErr := errors.New("main")
	closeErr := errors.New("closing")

	// Main success, closing failed
	res := noteCloseFailure(nil, description, closeErr)
	require.NotNil(t, res)
	assert.Contains(t, res.Error(), description)
	assert.Contains(t, res.Error(), closeErr.Error())

	// Both main and closing failed
	res = noteCloseFailure(mainErr, description, closeErr)
	require.NotNil(t, res)
	assert.Contains(t, res.Error(), mainErr.Error())
	assert.Contains(t, res.Error(), description)
	assert.Contains(t, res.Error(), closeErr.Error())
	assert.ErrorIs(t, res, mainErr)
}

// fakeGlobalOptions creates globalOptions and sets it according to flags.
func fakeGlobalOptions(t *testing.T, flags []string) (*globalOptions, *cobra.Command) {
	app, opts := createApp()
	cmd := &cobra.Command{}
	app.AddCommand(cmd)
	err := app.ParseFlags(flags)
	require.NoError(t, err)
	return opts, cmd
}

// fakeImageOptions creates imageOptions and sets it according to globalFlags/cmdFlags.
func fakeImageOptions(t *testing.T, flagPrefix string, useDeprecatedTLSVerify bool,
	globalFlags []string, cmdFlags []string) *imageOptions {
	globalOpts, cmd := fakeGlobalOptions(t, globalFlags)
	sharedFlags, sharedOpts := sharedImageFlags()
	var deprecatedTLSVerifyFlag pflag.FlagSet
	var deprecatedTLSVerifyOpt *deprecatedTLSVerifyOption
	if useDeprecatedTLSVerify {
		deprecatedTLSVerifyFlag, deprecatedTLSVerifyOpt = deprecatedTLSVerifyFlags()
	}
	imageFlags, imageOpts := imageFlags(globalOpts, sharedOpts, deprecatedTLSVerifyOpt, flagPrefix, "")
	cmd.Flags().AddFlagSet(&sharedFlags)
	if useDeprecatedTLSVerify {
		cmd.Flags().AddFlagSet(&deprecatedTLSVerifyFlag)
	}
	cmd.Flags().AddFlagSet(&imageFlags)
	err := cmd.ParseFlags(cmdFlags)
	require.NoError(t, err)
	return imageOpts
}

func TestImageOptionsNewSystemContext(t *testing.T) {
	// Default state
	opts := fakeImageOptions(t, "dest-", true, []string{}, []string{})
	res, err := opts.newSystemContext()
	require.NoError(t, err)
	assert.Equal(t, newExpectedSystemContext(), res)

	// Set everything to non-default values.
	opts = fakeImageOptions(t, "dest-", true, []string{
		"--registries.d", "/srv/registries.d",
		"--override-arch", "overridden-arch",
		"--override-os", "overridden-os",
		"--override-variant", "overridden-variant",
		"--tmpdir", "/srv",
	}, []string{
		"--authfile", "/srv/authfile",
		"--dest-authfile", "/srv/dest-authfile",
		"--dest-cert-dir", "/srv/cert-dir",
		"--dest-shared-blob-dir", "/srv/shared-blob-dir",
		"--dest-daemon-host", "daemon-host.example.com",
		"--dest-tls-verify=false",
		"--dest-creds", "creds-user:creds-password",
		"--dest-registry-token", "faketoken",
	})
	res, err = opts.newSystemContext()
	require.NoError(t, err)
	assert.Equal(t, &types.SystemContext{
		RegistriesDirPath:                 "/srv/registries.d",
		AuthFilePath:                      "/srv/dest-authfile",
		ArchitectureChoice:                "overridden-arch",
		OSChoice:                          "overridden-os",
		VariantChoice:                     "overridden-variant",
		OCISharedBlobDirPath:              "/srv/shared-blob-dir",
		DockerCertPath:                    "/srv/cert-dir",
		DockerInsecureSkipTLSVerify:       types.OptionalBoolTrue,
		DockerAuthConfig:                  &types.DockerAuthConfig{Username: "creds-user", Password: "creds-password"},
		DockerBearerRegistryToken:         "faketoken",
		DockerDaemonCertPath:              "/srv/cert-dir",
		DockerDaemonHost:                  "daemon-host.example.com",
		DockerDaemonInsecureSkipTLSVerify: true,
		DockerRegistryUserAgent:           defaultUserAgent,
		BigFilesTemporaryDir:              "/srv",
	}, res)

	// Global/per-command tlsVerify behavior is tested in TestTLSVerifyFlags.

	// Invalid option values
	opts = fakeImageOptions(t, "dest-", true, []string{}, []string{"--dest-creds", ""})
	_, err = opts.newSystemContext()
	assert.Error(t, err)
}

// fakeImageDestOptions creates imageDestOptions and sets it according to globalFlags/cmdFlags.
func fakeImageDestOptions(t *testing.T, flagPrefix string, useDeprecatedTLSVerify bool,
	globalFlags []string, cmdFlags []string) *imageDestOptions {
	globalOpts, cmd := fakeGlobalOptions(t, globalFlags)
	sharedFlags, sharedOpts := sharedImageFlags()
	var deprecatedTLSVerifyFlag pflag.FlagSet
	var deprecatedTLSVerifyOpt *deprecatedTLSVerifyOption
	if useDeprecatedTLSVerify {
		deprecatedTLSVerifyFlag, deprecatedTLSVerifyOpt = deprecatedTLSVerifyFlags()
	}
	imageFlags, imageOpts := imageDestFlags(globalOpts, sharedOpts, deprecatedTLSVerifyOpt, flagPrefix, "")
	cmd.Flags().AddFlagSet(&sharedFlags)
	if useDeprecatedTLSVerify {
		cmd.Flags().AddFlagSet(&deprecatedTLSVerifyFlag)
	}
	cmd.Flags().AddFlagSet(&imageFlags)
	err := cmd.ParseFlags(cmdFlags)
	require.NoError(t, err)
	return imageOpts
}

func TestImageDestOptionsNewSystemContext(t *testing.T) {
	// Default state
	opts := fakeImageDestOptions(t, "dest-", true, []string{}, []string{})
	res, err := opts.newSystemContext()
	require.NoError(t, err)
	assert.Equal(t, newExpectedSystemContext(), res)

	authFile := "/tmp/auth.json"
	// Make sure when REGISTRY_AUTH_FILE is set the auth file is used
	t.Setenv("REGISTRY_AUTH_FILE", authFile)

	// Explicitly set everything to default, except for when the default is “not present”
	opts = fakeImageDestOptions(t, "dest-", true, []string{}, []string{
		"--dest-compress=false",
	})
	res, err = opts.newSystemContext()
	require.NoError(t, err)
	expected := newExpectedSystemContext()
	expected.AuthFilePath = authFile
	assert.Equal(t, expected, res)

	// Set everything to non-default values.
	opts = fakeImageDestOptions(t, "dest-", true, []string{
		"--registries.d", "/srv/registries.d",
		"--override-arch", "overridden-arch",
		"--override-os", "overridden-os",
		"--override-variant", "overridden-variant",
		"--tmpdir", "/srv",
	}, []string{
		"--authfile", "/srv/authfile",
		"--dest-cert-dir", "/srv/cert-dir",
		"--dest-shared-blob-dir", "/srv/shared-blob-dir",
		"--dest-compress=true",
		"--dest-daemon-host", "daemon-host.example.com",
		"--dest-tls-verify=false",
		"--dest-creds", "creds-user:creds-password",
		"--dest-registry-token", "faketoken",
		"--dest-precompute-digests=true",
	})
	res, err = opts.newSystemContext()
	require.NoError(t, err)
	assert.Equal(t, &types.SystemContext{
		RegistriesDirPath:                   "/srv/registries.d",
		AuthFilePath:                        "/srv/authfile",
		ArchitectureChoice:                  "overridden-arch",
		OSChoice:                            "overridden-os",
		VariantChoice:                       "overridden-variant",
		OCISharedBlobDirPath:                "/srv/shared-blob-dir",
		DockerCertPath:                      "/srv/cert-dir",
		DockerInsecureSkipTLSVerify:         types.OptionalBoolTrue,
		DockerAuthConfig:                    &types.DockerAuthConfig{Username: "creds-user", Password: "creds-password"},
		DockerBearerRegistryToken:           "faketoken",
		DockerDaemonCertPath:                "/srv/cert-dir",
		DockerDaemonHost:                    "daemon-host.example.com",
		DockerDaemonInsecureSkipTLSVerify:   true,
		DockerRegistryUserAgent:             defaultUserAgent,
		DirForceCompress:                    true,
		BigFilesTemporaryDir:                "/srv",
		DockerRegistryPushPrecomputeDigests: true,
	}, res)

	// Global/per-command tlsVerify behavior is tested in TestTLSVerifyFlags.

	// Invalid option values in imageOptions
	opts = fakeImageDestOptions(t, "dest-", true, []string{}, []string{"--dest-creds", ""})
	_, err = opts.newSystemContext()
	assert.Error(t, err)
}

// TestImageOptionsUsernamePassword verifies that using the username and password
// options works as expected
func TestImageOptionsUsernamePassword(t *testing.T) {
	for _, command := range []struct {
		commandArgs        []string
		expectedAuthConfig *types.DockerAuthConfig // data to expect, or nil if an error is expected
	}{
		// Set only username/password (without --creds), expected to pass
		{
			commandArgs:        []string{"--dest-username", "foo", "--dest-password", "bar"},
			expectedAuthConfig: &types.DockerAuthConfig{Username: "foo", Password: "bar"},
		},
		// no username but set password, expect error
		{
			commandArgs:        []string{"--dest-password", "foo"},
			expectedAuthConfig: nil,
		},
		// set username but no password. expected to fail (we currently don't allow a user without password)
		{
			commandArgs:        []string{"--dest-username", "bar"},
			expectedAuthConfig: nil,
		},
		// set username with --creds, expected to fail
		{
			commandArgs:        []string{"--dest-username", "bar", "--dest-creds", "hello:world", "--dest-password", "foo"},
			expectedAuthConfig: nil,
		},
		// set username with --no-creds, expected to fail
		{
			commandArgs:        []string{"--dest-username", "bar", "--dest-no-creds", "--dest-password", "foo"},
			expectedAuthConfig: nil,
		},
	} {
		opts := fakeImageDestOptions(t, "dest-", true, []string{}, command.commandArgs)
		// parse the command options
		res, err := opts.newSystemContext()
		if command.expectedAuthConfig == nil {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			expected := newExpectedSystemContext()
			expected.DockerAuthConfig = command.expectedAuthConfig
			assert.Equal(t, expected, res)
		}
	}
}

func TestTLSVerifyFlags(t *testing.T) {
	type systemContextOpts interface { // Either *imageOptions or *imageDestOptions
		newSystemContext() (*types.SystemContext, error)
	}

	for _, creator := range []struct {
		name    string
		newOpts func(useDeprecatedTLSVerify bool, globalFlags, cmdFlags []string) systemContextOpts
	}{
		{
			"imageFlags",
			func(useDeprecatedTLSVerify bool, globalFlags, cmdFlags []string) systemContextOpts {
				return fakeImageOptions(t, "dest-", useDeprecatedTLSVerify, globalFlags, cmdFlags)
			},
		},
		{
			"imageDestFlags",
			func(useDeprecatedTLSVerify bool, globalFlags, cmdFlags []string) systemContextOpts {
				return fakeImageDestOptions(t, "dest-", useDeprecatedTLSVerify, globalFlags, cmdFlags)
			},
		},
	} {
		t.Run(creator.name, func(t *testing.T) {
			for _, c := range []struct {
				global, deprecatedCmd, cmd string
				expectedDocker             types.OptionalBool
				expectedDockerDaemon       bool
			}{
				{"", "", "", types.OptionalBoolUndefined, false},
				{"", "", "false", types.OptionalBoolTrue, true},
				{"", "", "true", types.OptionalBoolFalse, false},
				{"", "false", "", types.OptionalBoolTrue, false},
				{"", "false", "false", types.OptionalBoolTrue, true},
				{"", "false", "true", types.OptionalBoolFalse, false},
				{"", "true", "", types.OptionalBoolFalse, false},
				{"", "true", "false", types.OptionalBoolTrue, true},
				{"", "true", "true", types.OptionalBoolFalse, false},
				{"false", "", "", types.OptionalBoolTrue, false},
				{"false", "", "false", types.OptionalBoolTrue, true},
				{"false", "", "true", types.OptionalBoolFalse, false},
				{"false", "false", "", types.OptionalBoolTrue, false},
				{"false", "false", "false", types.OptionalBoolTrue, true},
				{"false", "false", "true", types.OptionalBoolFalse, false},
				{"false", "true", "", types.OptionalBoolFalse, false},
				{"false", "true", "false", types.OptionalBoolTrue, true},
				{"false", "true", "true", types.OptionalBoolFalse, false},
				{"true", "", "", types.OptionalBoolFalse, false},
				{"true", "", "false", types.OptionalBoolTrue, true},
				{"true", "", "true", types.OptionalBoolFalse, false},
				{"true", "false", "", types.OptionalBoolTrue, false},
				{"true", "false", "false", types.OptionalBoolTrue, true},
				{"true", "false", "true", types.OptionalBoolFalse, false},
				{"true", "true", "", types.OptionalBoolFalse, false},
				{"true", "true", "false", types.OptionalBoolTrue, true},
				{"true", "true", "true", types.OptionalBoolFalse, false},
			} {
				globalFlags := []string{}
				if c.global != "" {
					globalFlags = append(globalFlags, "--tls-verify="+c.global)
				}
				cmdFlags := []string{}
				if c.deprecatedCmd != "" {
					cmdFlags = append(cmdFlags, "--tls-verify="+c.deprecatedCmd)
				}
				if c.cmd != "" {
					cmdFlags = append(cmdFlags, "--dest-tls-verify="+c.cmd)
				}
				opts := creator.newOpts(true, globalFlags, cmdFlags)
				res, err := opts.newSystemContext()
				require.NoError(t, err)
				assert.Equal(t, c.expectedDocker, res.DockerInsecureSkipTLSVerify, "%#v", c)
				assert.Equal(t, c.expectedDockerDaemon, res.DockerDaemonInsecureSkipTLSVerify, "%#v", c)

				if c.deprecatedCmd == "" { // Test also the behavior when deprecatedTLSFlag is not recognized
					// Use globalFlags from the previous test
					cmdFlags := []string{}
					if c.cmd != "" {
						cmdFlags = append(cmdFlags, "--dest-tls-verify="+c.cmd)
					}
					opts := creator.newOpts(false, globalFlags, cmdFlags)
					res, err = opts.newSystemContext()
					require.NoError(t, err)
					assert.Equal(t, c.expectedDocker, res.DockerInsecureSkipTLSVerify, "%#v", c)
					assert.Equal(t, c.expectedDockerDaemon, res.DockerDaemonInsecureSkipTLSVerify, "%#v", c)
				}
			}
		})
	}
}

// fakeSharedCopyOptions creates sharedCopyOptions and sets it according to cmdFlags.
func fakeSharedCopyOptions(t *testing.T, cmdFlags []string) *sharedCopyOptions {
	_, cmd := fakeGlobalOptions(t, []string{})
	sharedCopyFlags, sharedCopyOpts := sharedCopyFlags()
	cmd.Flags().AddFlagSet(&sharedCopyFlags)
	err := cmd.ParseFlags(cmdFlags)
	require.NoError(t, err)
	return sharedCopyOpts
}

func TestSharedCopyOptionsCopyOptions(t *testing.T) {
	someStdout := bytes.Buffer{}

	// Default state
	opts := fakeSharedCopyOptions(t, []string{})
	res, cleanup, err := opts.copyOptions(&someStdout)
	require.NoError(t, err)
	defer cleanup()
	assert.Equal(t, &copy.Options{
		ReportWriter: &someStdout,
	}, res)

	// Set most flags to non-default values
	// This should also test --sign-by-sigstore and --sign-by-sigstore-private-key; we would have
	// to create test keys for that.
	opts = fakeSharedCopyOptions(t, []string{
		"--remove-signatures",
		"--sign-by", "gpgFingerprint",
		"--format", "oci",
		"--preserve-digests",
	})
	res, cleanup, err = opts.copyOptions(&someStdout)
	require.NoError(t, err)
	defer cleanup()
	assert.Equal(t, &copy.Options{
		RemoveSignatures:      true,
		SignBy:                "gpgFingerprint",
		ReportWriter:          &someStdout,
		PreserveDigests:       true,
		ForceManifestMIMEType: imgspecv1.MediaTypeImageManifest,
	}, res)

	// --sign-passphrase-file + --sign-by work
	passphraseFile, err := os.CreateTemp("", "passphrase") // Eventually we could refer to a passphrase fixture instead
	require.NoError(t, err)
	defer os.Remove(passphraseFile.Name())
	_, err = passphraseFile.WriteString("test-passphrase")
	require.NoError(t, err)
	opts = fakeSharedCopyOptions(t, []string{
		"--sign-by", "gpgFingerprint",
		"--sign-passphrase-file", passphraseFile.Name(),
	})
	res, cleanup, err = opts.copyOptions(&someStdout)
	require.NoError(t, err)
	defer cleanup()
	assert.Equal(t, &copy.Options{
		SignBy:                           "gpgFingerprint",
		SignPassphrase:                   "test-passphrase",
		SignSigstorePrivateKeyPassphrase: []byte("test-passphrase"),
		ReportWriter:                     &someStdout,
	}, res)
	// --sign-passphrase-file + --sign-by-sigstore-private-key should be tested here.

	// Invalid --format
	opts = fakeSharedCopyOptions(t, []string{"--format", "invalid"})
	_, _, err = opts.copyOptions(&someStdout)
	assert.Error(t, err)

	// More --sign-passphrase-file, --sign-by-sigstore-private-key, --sign-by-sigstore failure cases should be tested here.

	// --sign-passphrase-file not found
	opts = fakeSharedCopyOptions(t, []string{
		"--sign-by", "gpgFingerprint",
		"--sign-passphrase-file", "/dev/null/this/does/not/exist",
	})
	_, _, err = opts.copyOptions(&someStdout)
	assert.Error(t, err)

	// --sign-by-sigstore file not found
	opts = fakeSharedCopyOptions(t, []string{
		"--sign-by-sigstore", "/dev/null/this/does/not/exist",
	})
	_, _, err = opts.copyOptions(&someStdout)
	assert.Error(t, err)
}

func TestParseManifestFormat(t *testing.T) {
	for _, testCase := range []struct {
		formatParam          string
		expectedManifestType string
		expectErr            bool
	}{
		{"oci",
			imgspecv1.MediaTypeImageManifest,
			false},
		{"v2s1",
			manifest.DockerV2Schema1SignedMediaType,
			false},
		{"v2s2",
			manifest.DockerV2Schema2MediaType,
			false},
		{"",
			"",
			true},
		{"badValue",
			"",
			true},
	} {
		manifestType, err := parseManifestFormat(testCase.formatParam)
		if testCase.expectErr {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		assert.Equal(t, manifestType, testCase.expectedManifestType)
	}
}

// since there is a shared authfile image option and a non-shared (prefixed) one, make sure the override logic
// works correctly.
func TestImageOptionsAuthfileOverride(t *testing.T) {
	for _, testCase := range []struct {
		flagPrefix           string
		cmdFlags             []string
		expectedAuthfilePath string
	}{
		// if there is no prefix, only authfile is allowed.
		{"",
			[]string{
				"--authfile", "/srv/authfile",
			}, "/srv/authfile"},
		// if authfile and dest-authfile is provided, dest-authfile wins
		{"dest-",
			[]string{
				"--authfile", "/srv/authfile",
				"--dest-authfile", "/srv/dest-authfile",
			}, "/srv/dest-authfile",
		},
		// if only the shared authfile is provided, authfile must be present in system context
		{"dest-",
			[]string{
				"--authfile", "/srv/authfile",
			}, "/srv/authfile",
		},
		// if only the dest authfile is provided, dest-authfile must be present in system context
		{"dest-",
			[]string{
				"--dest-authfile", "/srv/dest-authfile",
			}, "/srv/dest-authfile",
		},
	} {
		opts := fakeImageOptions(t, testCase.flagPrefix, false, []string{}, testCase.cmdFlags)
		res, err := opts.newSystemContext()
		require.NoError(t, err)

		expected := &types.SystemContext{
			AuthFilePath:            testCase.expectedAuthfilePath,
			DockerRegistryUserAgent: defaultUserAgent,
		}
		// On macOS, DockerDaemonHost gets set to the platform-specific default
		if runtime.GOOS == "darwin" {
			if home := os.Getenv("HOME"); home != "" {
				expected.DockerDaemonHost = "unix://" + filepath.Join(home, ".docker/run/docker.sock")
			}
		}
		assert.Equal(t, expected, res)
	}
}
