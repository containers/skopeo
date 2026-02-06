package tlsdetails

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TLSDetailsFile contains a set of TLS options.
//
// To consume such a file, most callers should use c/image/pkg/cli/basetls instead
// of dealing with this type explicitly using ParseFile.
//
// This type is exported primarily to allow creating parameter files programmatically
// (and eventually this subpackage should provide an API to convert this type into
// the appropriate file contents, so that callers don't need to do that manually).
type TLSDetailsFile struct {
	// Keep this in sync with docs/containers-tls-details.yaml.5.md !

	MinVersion   string   `yaml:"minVersion,omitempty"`   // If set, minimum version to use throughout the program.
	CipherSuites []string `yaml:"cipherSuites,omitempty"` // If set, allowed TLS cipher suites to use throughout the program.
	NamedGroups  []string `yaml:"namedGroups,omitempty"`  // If set, allowed TLS named groups to use throughout the program.
}

// ParseFile parses a TLSDetailsFile at the specified path.
//
// Most consumers of the parameter file should use c/image/pkg/cli/basetls instead.
func ParseFile(path string) (*TLSDetailsFile, error) {
	var res TLSDetailsFile
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(source))
	dec.KnownFields(true)
	if err = dec.Decode(&res); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	return &res, nil
}
