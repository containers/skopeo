package main

import (
	"os"
	"runtime"
	"testing"

	"github.com/containers/storage/pkg/reexec"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}

func TestParseMultiArchSparse(t *testing.T) {
	digestA := digest.Canonical.FromBytes([]byte("A"))
	digestB := digest.Canonical.FromBytes([]byte("B"))
	testCases := []struct {
		description                               string
		overrideOS, overrideArch, overrideVariant string
		multiArchArg                              string
		expectedPlatforms                         []imgspecv1.Platform
		expectedDigests                           []digest.Digest
		expectedErrorFragment                     string
	}{
		{
			description:           "empty value",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "one comma",
			multiArchArg:          ",",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "two commas",
			multiArchArg:          ",,",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "bogus bare value",
			multiArchArg:          "vegetables=artichokes",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "bogus value short list",
			multiArchArg:          "vegetables=[artichokes]",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "bogus value long list",
			multiArchArg:          "brassica=[arugula,broccoli,cauliflower,daikon]",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:  "basic system",
			multiArchArg: "system",
			expectedPlatforms: []imgspecv1.Platform{
				{},
			},
		},
		{
			description:  "system with OS",
			overrideOS:   "someOS",
			multiArchArg: "system",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS: "someOS",
				},
			},
		},
		{
			description:  "system with arch",
			overrideArch: "someArch",
			multiArchArg: "system",
			expectedPlatforms: []imgspecv1.Platform{
				{
					Architecture: "someArch",
				},
			},
		},
		{
			description:  "system with both OS and arch",
			overrideOS:   "someOS",
			overrideArch: "someArch",
			multiArchArg: "system",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "someOS",
					Architecture: "someArch",
				},
			},
		},
		{
			description:  "arch short list",
			overrideOS:   "someOS",
			overrideArch: "someArch",
			multiArchArg: "arch=[amd64]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "someOS",
					Architecture: "amd64",
				},
			},
		},
		{
			description:  "arch longer list",
			overrideOS:   "someOS",
			overrideArch: "someArch",
			multiArchArg: "arch=[amd64,ppc64le]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "someOS",
					Architecture: "amd64",
				},
				{
					OS:           "someOS",
					Architecture: "ppc64le",
				},
			},
		},
		{
			description:  "arch defaulted list",
			multiArchArg: "arch=[amd64,s390x,ppc64le]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           runtime.GOOS,
					Architecture: "amd64",
				},
				{
					OS:           runtime.GOOS,
					Architecture: "s390x",
				},
				{
					OS:           runtime.GOOS,
					Architecture: "ppc64le",
				},
			},
		},
		{
			description:           "arch broken list missing opener",
			multiArchArg:          "arch=amd64,s390x,ppc64le]",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "arch broken list missing closer",
			multiArchArg:          "arch=[amd64,s390x,ppc64le",
			expectedErrorFragment: "] not found",
		},
		{
			description:     "digest short list",
			multiArchArg:    "digest=[" + digestA.String() + "]",
			expectedDigests: []digest.Digest{digestA},
		},
		{
			description:     "digest longer list",
			multiArchArg:    "digest=[" + digestA.String() + "," + digestB.String() + "]",
			expectedDigests: []digest.Digest{digestA, digestB},
		},
		{
			description:           "digest broken list missing opener",
			multiArchArg:          "digest=" + digestA.String() + "]",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "digest broken list missing closer",
			multiArchArg:          "digest=[" + digestA.String(),
			expectedErrorFragment: "] not found",
		},
		{
			description:  "platform short list",
			multiArchArg: "platform=[linux/riscv]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "linux",
					Architecture: "riscv",
				},
			},
		},
		{
			description:  "platform longer list",
			multiArchArg: "platform=[linux/riscv,windows/riscv,linux/ppc64le]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "linux",
					Architecture: "riscv",
				},
				{
					OS:           "windows",
					Architecture: "riscv",
				},
				{
					OS:           "linux",
					Architecture: "ppc64le",
				},
			},
		},
		{
			description:           "platform broken list missing opener",
			multiArchArg:          "platform=linux/riscv]",
			expectedErrorFragment: "unrecognized value",
		},
		{
			description:           "platform broken list missing closer",
			multiArchArg:          "platform=[linux/riscv",
			expectedErrorFragment: "] not found",
		},
		{
			description:  "mixed",
			overrideOS:   "someOS",
			overrideArch: "someArch",
			multiArchArg: "platform=[linux/riscv,windows/riscv],arch=[amd64,ppc64le],digest=[" + digestA.String() + "," + digestB.String() + "]",
			expectedPlatforms: []imgspecv1.Platform{
				{
					OS:           "linux",
					Architecture: "riscv",
				},
				{
					OS:           "windows",
					Architecture: "riscv",
				},
				{
					OS:           "someOS",
					Architecture: "amd64",
				},
				{
					OS:           "someOS",
					Architecture: "ppc64le",
				},
			},
			expectedDigests: []digest.Digest{digestA, digestB},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			globalOptions := globalOptions{
				overrideOS:      tc.overrideOS,
				overrideArch:    tc.overrideArch,
				overrideVariant: tc.overrideVariant,
			}
			instancePlatforms, instanceDigests, err := parseMultiArchSparse(&globalOptions, tc.multiArchArg)
			if err != nil {
				require.NotEmptyf(t, tc.expectedErrorFragment, "unexpected error parsing %q: %v", tc.multiArchArg, err)
				require.Contains(t, err.Error(), tc.expectedErrorFragment)
			}
			assert.Equal(t, tc.expectedDigests, instanceDigests)
			assert.Equal(t, tc.expectedPlatforms, instancePlatforms)
		})
	}

}
