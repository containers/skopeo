package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const skopeoBinary = "skopeo"

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

// fileFromFixture applies edits to inputPath and returns a path to the temporary file.
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

// decompressDirs decompresses specified dir:-formatted directories
func decompressDirs(t *testing.T, dirs ...string) {
	t.Logf("Decompressing %s", strings.Join(dirs, " "))
	for i, dir := range dirs {
		m, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		t.Logf("manifest %d before: %s", i+1, string(m))

		decompressDir(t, dir)

		m, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		t.Logf("manifest %d after: %s", i+1, string(m))
	}
}

// getRawMapField assigns a value of rawMap[key] to dest,
// failing if it does not exist or if it doesn’t have the expected type
func getRawMapField[T any](t *testing.T, rawMap map[string]any, key string, dest *T) {
	rawValue, ok := rawMap[key]
	require.True(t, ok, key)
	value, ok := rawValue.(T)
	require.True(t, ok, key, "%#v", value)
	*dest = value
}

// decompressDir modifies a dir:-formatted directory to replace gzip-compressed layers with uncompressed variants,
// and to use a ~canonical formatting of manifest.json.
func decompressDir(t *testing.T, dir string) {
	// This is, overall, very dumb; the “obvious” way would be to invoke skopeo to decompress,
	// or at least to use c/image to parse/format the manifest.
	//
	// But this is used to test (aspects of) those code paths… so, it’s acceptable for this to be
	// dumb and to make assumptions about the data, but it should not share code.

	manifestBlob, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var rawManifest map[string]any
	err = json.Unmarshal(manifestBlob, &rawManifest)
	require.NoError(t, err)
	var rawLayers []any
	getRawMapField(t, rawManifest, "layers", &rawLayers)
	for i, rawLayerValue := range rawLayers {
		rawLayer, ok := rawLayerValue.(map[string]any)
		require.True(t, ok)
		var digestString string
		getRawMapField(t, rawLayer, "digest", &digestString)
		compressedDigest, err := digest.Parse(digestString)
		require.NoError(t, err)
		if compressedDigest.String() == "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" { // An empty file
			continue
		}

		compressedPath := filepath.Join(dir, compressedDigest.Encoded())
		compressedStream, err := os.Open(compressedPath)
		require.NoError(t, err)
		defer compressedStream.Close()

		uncompressedStream, err := gzip.NewReader(compressedStream)
		if err != nil {
			continue // Silently assume the layer is not gzip-compressed
		}
		tempDest, err := os.CreateTemp(dir, "decompressing")
		require.NoError(t, err)
		digester := digest.Canonical.Digester()
		uncompressedSize, err := io.Copy(tempDest, io.TeeReader(uncompressedStream, digester.Hash()))
		require.NoError(t, err)
		err = uncompressedStream.Close()
		require.NoError(t, err)
		uncompressedDigest := digester.Digest()
		uncompressedPath := filepath.Join(dir, uncompressedDigest.Encoded())
		err = os.Rename(tempDest.Name(), uncompressedPath)
		require.NoError(t, err)
		err = os.Remove(compressedPath)
		require.NoError(t, err)

		rawLayer["digest"] = uncompressedDigest.String()
		rawLayer["size"] = uncompressedSize
		var mimeType string
		getRawMapField(t, rawLayer, "mediaType", &mimeType)
		if uncompressedMIMEType, ok := strings.CutSuffix(mimeType, ".gzip"); ok {
			rawLayer["mediaType"] = uncompressedMIMEType
		}

		rawLayers[i] = rawLayer
	}
	rawManifest["layers"] = rawLayers

	manifestBlob, err = json.Marshal(rawManifest)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBlob, 0o600)
	require.NoError(t, err)
}

// Verify manifest in a dir: image at dir is expectedMIMEType.
func verifyManifestMIMEType(t *testing.T, dir string, expectedMIMEType string) {
	manifestBlob, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	mimeType := manifest.GuessMIMEType(manifestBlob)
	assert.Equal(t, expectedMIMEType, mimeType)
}
