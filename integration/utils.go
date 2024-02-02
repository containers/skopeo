package main

import (
	"bytes"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const skopeoBinary = "skopeo"
const decompressDirsBinary = "./decompress-dirs.sh"

const testFQIN = "docker://quay.io/libpod/busybox" // tag left off on purpose, some tests need to add a special one
const testFQIN64 = "docker://quay.io/libpod/busybox:amd64"
const testFQINMultiLayer = "docker://quay.io/libpod/alpine_nginx:latest" // multi-layer

// consumeAndLogOutputStream takes (f, err) from an exec.*Pipe(), and causes all output to it to be logged to t.
func consumeAndLogOutputStream(t *testing.T, id string, f io.ReadCloser, err error) {
	require.NoError(t, err)
	go func() {
		defer func() {
			f.Close()
			t.Logf("Output %s: Closed", id)
		}()
		buf := make([]byte, 1024)
		for {
			t.Logf("Output %s: waiting", id)
			n, err := f.Read(buf)
			t.Logf("Output %s: got %d,%#v: %s", id, n, err, strings.TrimSuffix(string(buf[:n]), "\n"))
			if n <= 0 {
				break
			}
		}
	}()
}

// consumeAndLogOutputs causes all output to stdout and stderr from an *exec.Cmd to be logged to c.
func consumeAndLogOutputs(t *testing.T, id string, cmd *exec.Cmd) {
	stdout, err := cmd.StdoutPipe()
	consumeAndLogOutputStream(t, id+" stdout", stdout, err)
	stderr, err := cmd.StderrPipe()
	consumeAndLogOutputStream(t, id+" stderr", stderr, err)
}

// combinedOutputOfCommand runs a command as if exec.Command().CombinedOutput(), verifies that the exit status is 0, and returns the output,
// or terminates c on failure.
func combinedOutputOfCommand(t *testing.T, name string, args ...string) string {
	t.Logf("Running %s %s", name, strings.Join(args, " "))
	out, err := exec.Command(name, args...).CombinedOutput()
	require.NoError(t, err, "%s", out)
	return string(out)
}

// assertSkopeoSucceeds runs a skopeo command as if exec.Command().CombinedOutput, verifies that the exit status is 0,
// and optionally that the output matches a multi-line regexp if it is nonempty;
// or terminates c on failure
func assertSkopeoSucceeds(t *testing.T, regexp string, args ...string) {
	t.Logf("Running %s %s", skopeoBinary, strings.Join(args, " "))
	out, err := exec.Command(skopeoBinary, args...).CombinedOutput()
	assert.NoError(t, err, "%s", out)
	if regexp != "" {
		assert.Regexp(t, "(?s)"+regexp, string(out)) // (?s) : '.' will also match newlines
	}
}

// assertSkopeoFails runs a skopeo command as if exec.Command().CombinedOutput, verifies that the exit status is 0,
// and that the output matches a multi-line regexp;
// or terminates c on failure
func assertSkopeoFails(t *testing.T, regexp string, args ...string) {
	t.Logf("Running %s %s", skopeoBinary, strings.Join(args, " "))
	out, err := exec.Command(skopeoBinary, args...).CombinedOutput()
	assert.Error(t, err, "%s", out)
	assert.Regexp(t, "(?s)"+regexp, string(out)) // (?s) : '.' will also match newlines
}

// runCommandWithInput runs a command as if exec.Command(), sending it the input to stdin,
// and verifies that the exit status is 0, or terminates c on failure.
func runCommandWithInput(t *testing.T, input string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	runExecCmdWithInput(t, cmd, input)
}

// runExecCmdWithInput runs an exec.Cmd, sending it the input to stdin,
// and verifies that the exit status is 0, or terminates c on failure.
func runExecCmdWithInput(t *testing.T, cmd *exec.Cmd, input string) {
	t.Logf("Running %s %s", cmd.Path, strings.Join(cmd.Args, " "))
	consumeAndLogOutputs(t, cmd.Path+" "+strings.Join(cmd.Args, " "), cmd)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	err = cmd.Start()
	require.NoError(t, err)
	_, err = io.WriteString(stdin, input)
	require.NoError(t, err)
	err = stdin.Close()
	require.NoError(t, err)
	err = cmd.Wait()
	assert.NoError(t, err)
}

// isPortOpen returns true iff the specified port on localhost is open.
func isPortOpen(port uint16) bool {
	ap := netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port)
	conn, err := net.DialTCP("tcp", nil, net.TCPAddrFromAddrPort(ap))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// newPortChecker sets up a portOpen channel which will receive true after the specified port is open.
// The checking can be aborted by sending a value to the terminate channel, which the caller should
// always do using
// defer func() {terminate <- true}()
func newPortChecker(t *testing.T, port uint16) (portOpen <-chan bool, terminate chan<- bool) {
	portOpenBidi := make(chan bool)
	// Buffered, so that sending a terminate request after the goroutine has exited does not block.
	terminateBidi := make(chan bool, 1)

	go func() {
		defer func() {
			t.Logf("Port checker for port %d exiting", port)
		}()
		for {
			t.Logf("Checking for port %d...", port)
			if isPortOpen(port) {
				t.Logf("Port %d open", port)
				portOpenBidi <- true
				return
			}
			t.Logf("Sleeping for port %d", port)
			sleepChan := time.After(100 * time.Millisecond)
			select {
			case <-sleepChan: // Try again
				t.Logf("Sleeping for port %d done, will retry", port)
			case <-terminateBidi:
				t.Logf("Check for port %d terminated", port)
				return
			}
		}
	}()
	return portOpenBidi, terminateBidi
}

// modifyEnviron modifies os.Environ()-like list of name=value assignments to set name to value.
func modifyEnviron(env []string, name, value string) []string {
	prefix := name + "="
	res := []string{}
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			res = append(res, e)
		}
	}
	return append(res, prefix+value)
}

// fileFromFixtureFixture applies edits to inputPath and returns a path to the temporary file.
// Callers should defer os.Remove(the_returned_path)
func fileFromFixture(t *testing.T, inputPath string, edits map[string]string) string {
	contents, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	for template, value := range edits {
		updated := bytes.ReplaceAll(contents, []byte(template), []byte(value))
		require.NotEqual(t, contents, updated, "Replacing %s in %#v failed", template, string(contents)) // Verify that the template has matched something and we are not silently ignoring it.
		contents = updated
	}

	file, err := os.CreateTemp("", "policy.json")
	require.NoError(t, err)
	path := file.Name()

	_, err = file.Write(contents)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)
	return path
}

// runDecompressDirs runs decompress-dirs.sh using exec.Command().CombinedOutput, verifies that the exit status is 0,
// and optionally that the output matches a multi-line regexp if it is nonempty; or terminates c on failure
func runDecompressDirs(t *testing.T, args ...string) {
	t.Logf("Running %s %s", decompressDirsBinary, strings.Join(args, " "))
	for i, dir := range args {
		m, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		t.Logf("manifest %d before: %s", i+1, string(m))

		decompressDir(t, dir)

		m, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		t.Logf("manifest %d after: %s", i+1, string(m))
	}
}

// decompressDir modifies a dir:-formatted directory to replace gzip-compressed layers with uncompressed variants,
// and to use a ~canonical formatting of manifest.json.
func decompressDir(t *testing.T, dir string) {
	out, err := exec.Command(decompressDirsBinary, dir).CombinedOutput()
	assert.NoError(t, err, "%s", out)
	if len(out) > 0 {
		t.Logf("output: %s", out)
	}
}

// Verify manifest in a dir: image at dir is expectedMIMEType.
func verifyManifestMIMEType(t *testing.T, dir string, expectedMIMEType string) {
	manifestBlob, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	mimeType := manifest.GuessMIMEType(manifestBlob)
	assert.Equal(t, expectedMIMEType, mimeType)
}
