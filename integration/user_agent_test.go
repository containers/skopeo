package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockRegistryHandler implements a minimal Docker Registry V2 API that captures User-Agent headers
type mockRegistryHandler struct {
	mu         sync.Mutex
	userAgents []string
}

func (h *mockRegistryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Capture the User-Agent header
	h.mu.Lock()
	h.userAgents = append(h.userAgents, r.Header.Get("User-Agent"))
	h.mu.Unlock()

	// Implement minimal Docker Registry V2 API endpoints for inspect --raw
	switch {
	case r.URL.Path == "/v2/":
		// Registry version check endpoint
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)

	case strings.HasSuffix(r.URL.Path, "/manifests/latest"):
		// Return a minimal OCI manifest as raw string
		// The digest matches this exact content
		manifest := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","size":0}]}`
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(manifest)); err != nil {
			panic(err)
		}

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *mockRegistryHandler) getUserAgents() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.userAgents)
}

func TestUserAgent(t *testing.T) {
	testCases := []struct {
		name               string
		extraArgs          []string
		userAgentValidator func(string) bool
		description        string
	}{
		{
			name:      "default user agent",
			extraArgs: []string{},
			userAgentValidator: func(ua string) bool {
				return strings.HasPrefix(ua, "skopeo/")
			},
			description: "Default user agent should start with 'skopeo/'",
		},
		{
			name:      "custom user agent prefix",
			extraArgs: []string{"--user-agent-prefix", "bootc/1.0"},
			userAgentValidator: func(ua string) bool {
				return strings.HasPrefix(ua, "bootc/1.0 skopeo/")
			},
			description: "Custom user agent should be in format 'prefix skopeo/version'",
		},
		{
			name:      "prefix with spaces",
			extraArgs: []string{"--user-agent-prefix", "my cool app"},
			userAgentValidator: func(ua string) bool {
				return strings.HasPrefix(ua, "my cool app skopeo/")
			},
			description: "User agent with spaces should work correctly",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &mockRegistryHandler{}
			server := httptest.NewServer(handler)
			defer server.Close()

			// Extract host:port from the test server URL
			registryAddr := strings.TrimPrefix(server.URL, "http://")
			imageRef := "docker://" + registryAddr + "/test/image:latest"

			// Build arguments: base args + test-specific args + image ref
			args := append([]string{"--tls-verify=false"}, tc.extraArgs...)
			args = append(args, "inspect", "--raw", imageRef)

			// Run skopeo inspect --raw
			assertSkopeoSucceeds(t, "", args...)

			// Verify that at least one request was made with the expected User-Agent
			userAgents := handler.getUserAgents()
			require.NotEmpty(t, userAgents, "Expected at least one request to be made")

			// Check that at least one User-Agent matches the validator
			require.True(t,
				slices.ContainsFunc(userAgents, tc.userAgentValidator),
				"%s, got: %v", tc.description, userAgents)
		})
	}
}
