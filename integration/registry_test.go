package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	binaryV2        = "registry-v2"
	binaryV2Schema1 = "registry-v2-schema1"
)

type testRegistryV2 struct {
	cmd      *exec.Cmd
	url      string
	username string
	password string
	email    string
}

func setupRegistryV2At(t *testing.T, url string, auth, schema1 bool) *testRegistryV2 {
	reg, err := newTestRegistryV2At(t, url, auth, schema1)
	require.NoError(t, err)

	// Wait for registry to be ready to serve requests.
	for range 50 {
		if err = reg.Ping(); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err != nil {
		t.Fatal("Timeout waiting for test registry to become available")
	}
	return reg
}

func newTestRegistryV2At(t *testing.T, url string, auth, schema1 bool) (*testRegistryV2, error) {
	tmp := t.TempDir()
	template := `version: 0.1
loglevel: debug
storage:
    filesystem:
        rootdirectory: %s
    delete:
        enabled: true
http:
    addr: %s
compatibility:
    schema1:
        enabled: true
%s`
	var (
		htpasswd string
		username string
		password string
		email    string
	)
	if auth {
		htpasswdPath := filepath.Join(tmp, "htpasswd")
		userpasswd := "testuser:$2y$05$sBsSqk0OpSD1uTZkHXc4FeJ0Z70wLQdAX/82UiHuQOKbNbBrzs63m"
		username = "testuser"
		password = "testpassword"
		email = "test@test.org"
		if err := os.WriteFile(htpasswdPath, []byte(userpasswd), os.FileMode(0644)); err != nil {
			return nil, err
		}
		htpasswd = fmt.Sprintf(`auth:
    htpasswd:
        realm: basic-realm
        path: %s
`, htpasswdPath)
	}
	confPath := filepath.Join(tmp, "config.yaml")
	config, err := os.Create(confPath)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(config, template, tmp, url, htpasswd); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if schema1 {
		cmd = exec.Command(binaryV2Schema1, confPath)
	} else {
		cmd = exec.Command(binaryV2, "serve", confPath)
	}

	consumeAndLogOutputs(t, fmt.Sprintf("registry-%s", url), cmd)
	if err := cmd.Start(); err != nil {
		if os.IsNotExist(err) {
			t.Skip(err.Error())
		}
		return nil, err
	}
	return &testRegistryV2{
		cmd:      cmd,
		url:      url,
		username: username,
		password: password,
		email:    email,
	}, nil
}

func (r *testRegistryV2) Ping() error {
	// We always ping through HTTP for our test registry.
	resp, err := http.Get(fmt.Sprintf("http://%s/v2/", r.url))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("registry ping replied with an unexpected status code %d", resp.StatusCode)
	}
	return nil
}

func (r *testRegistryV2) tearDown() {
	// It’s undocumented what Kill() returns if the process has terminated,
	// so we couldn’t check just for that. This is running in a container anyway…
	_ = r.cmd.Process.Kill()
}
