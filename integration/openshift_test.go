package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/containers/storage/pkg/homedir"
	"github.com/stretchr/testify/require"
)

var adminKUBECONFIG = map[string]string{
	"KUBECONFIG": "openshift.local.config/master/admin.kubeconfig",
}

// openshiftCluster is an OpenShift API master and integrated registry
// running on localhost.
type openshiftCluster struct {
	workingDir string
	dockerDir  string
	processes  []*exec.Cmd // Processes to terminate on teardown; append to the end, terminate from end to the start.
}

// startOpenshiftCluster creates a new openshiftCluster.
// WARNING: This affects state in users' home directory! Only run
// in isolated test environment.
func startOpenshiftCluster(t *testing.T) *openshiftCluster {
	cluster := &openshiftCluster{}
	cluster.workingDir = t.TempDir()

	cluster.startMaster(t)
	cluster.prepareRegistryConfig(t)
	cluster.startRegistry(t)
	cluster.ocLoginToProject(t)
	cluster.dockerLogin(t)
	cluster.relaxImageSignerPermissions(t)

	return cluster
}

// clusterCmd creates an exec.Cmd in cluster.workingDir with current environment modified by environment.
func (cluster *openshiftCluster) clusterCmd(env map[string]string, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = cluster.workingDir
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = modifyEnviron(cmd.Env, key, value)
	}
	return cmd
}

// startMaster starts the OpenShift master (etcd+API server) and waits for it to be ready, or terminates on failure.
func (cluster *openshiftCluster) startMaster(t *testing.T) {
	cmd := cluster.clusterCmd(nil, "openshift", "start", "master")
	cluster.processes = append(cluster.processes, cmd)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	// but logging actually goes only to stderr - this primarily ensure we log any
	// unexpected output to stdout.
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	require.NoError(t, err)

	portOpen, terminatePortCheck := newPortChecker(t, 8443)
	defer func() {
		t.Logf("Terminating port check")
		terminatePortCheck <- true
	}()

	terminateLogCheck := make(chan bool, 1)
	logCheckFound := make(chan bool)
	go func() {
		defer func() {
			t.Logf("Log checker exiting")
		}()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("Log line: %s", line)
			if strings.Contains(line, "Started Origin Controllers") {
				logCheckFound <- true
				return
				// FIXME? We stop reading from stdout; could this block the master?
			}
			// Note: we can block before we get here.
			select {
			case <-terminateLogCheck:
				t.Logf("terminated")
				return
			default:
				// Do not block here and read the next line.
			}
		}
		logCheckFound <- false
	}()
	defer func() {
		t.Logf("Terminating log check")
		terminateLogCheck <- true
	}()

	gotPortCheck := false
	gotLogCheck := false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for !gotPortCheck || !gotLogCheck {
		t.Logf("Waiting for master")
		select {
		case <-portOpen:
			t.Logf("port check done")
			gotPortCheck = true
		case found := <-logCheckFound:
			t.Logf("log check done, found: %t", found)
			if !found {
				t.Fatal("log check done, success message not found")
			}
			gotLogCheck = true
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for master: %v", ctx.Err())
		}
	}
	t.Logf("OK, master started!")
}

// prepareRegistryConfig creates a registry service account and a related k8s client configuration in ${cluster.workingDir}/openshift.local.registry.
func (cluster *openshiftCluster) prepareRegistryConfig(t *testing.T) {
	// This partially mimics the objects created by (oadm registry), except that we run the
	// server directly as an ordinary process instead of a pod with an implicitly attached service account.
	saJSON := `{
		"apiVersion": "v1",
		"kind": "ServiceAccount",
		"metadata": {
			"name": "registry"
		}
	}`
	cmd := cluster.clusterCmd(adminKUBECONFIG, "oc", "create", "-f", "-")
	runExecCmdWithInput(t, cmd, saJSON)

	cmd = cluster.clusterCmd(adminKUBECONFIG, "oadm", "policy", "add-cluster-role-to-user", "system:registry", "-z", "registry")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", string(out))
	require.Equal(t, "cluster role \"system:registry\" added: \"registry\"\n", string(out))

	cmd = cluster.clusterCmd(adminKUBECONFIG, "oadm", "create-api-client-config", "--client-dir=openshift.local.registry", "--basename=openshift-registry", "--user=system:serviceaccount:default:registry")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "%s", string(out))
	require.Equal(t, "", string(out))
}

// startRegistryProcess starts the OpenShift registry with configPart on port, waits for it to be ready, and returns the process object, or terminates on failure.
func (cluster *openshiftCluster) startRegistryProcess(t *testing.T, port uint16, configPath string) *exec.Cmd {
	cmd := cluster.clusterCmd(map[string]string{
		"KUBECONFIG":          "openshift.local.registry/openshift-registry.kubeconfig",
		"DOCKER_REGISTRY_URL": fmt.Sprintf("127.0.0.1:%d", port),
	}, "dockerregistry", configPath)
	consumeAndLogOutputs(t, fmt.Sprintf("registry-%d", port), cmd)
	err := cmd.Start()
	require.NoError(t, err, "%s")

	portOpen, terminatePortCheck := newPortChecker(t, port)
	defer func() {
		terminatePortCheck <- true
	}()
	t.Logf("Waiting for registry to start")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	select {
	case <-portOpen:
		t.Logf("OK, Registry port open")
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for registry to start: %v", ctx.Err())
	}

	return cmd
}

// startRegistry starts the OpenShift registry and waits for it to be ready, or terminates on failure.
func (cluster *openshiftCluster) startRegistry(t *testing.T) {
	// Our “primary” registry
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(t, 5000, "/atomic-registry-config.yml"))

	// A registry configured with acceptschema2:false
	schema1Config := fileFromFixture(t, "/atomic-registry-config.yml", map[string]string{
		"addr: :5000":              "addr: :5005",
		"rootdirectory: /registry": "rootdirectory: /registry-schema1",
		// The default configuration currently already contains acceptschema2: false
	})
	// Make sure the configuration contains "acceptschema2: false", because eventually it will be enabled upstream and this function will need to be updated.
	configContents, err := os.ReadFile(schema1Config)
	require.NoError(t, err)
	require.Regexp(t, "(?s).*acceptschema2: false.*", string(configContents))
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(t, 5005, schema1Config))

	// A registry configured with acceptschema2:true
	schema2Config := fileFromFixture(t, "/atomic-registry-config.yml", map[string]string{
		"addr: :5000":              "addr: :5006",
		"rootdirectory: /registry": "rootdirectory: /registry-schema2",
		"acceptschema2: false":     "acceptschema2: true",
	})
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(t, 5006, schema2Config))
}

// ocLogin runs (oc login) and (oc new-project) on the cluster, or terminates on failure.
func (cluster *openshiftCluster) ocLoginToProject(t *testing.T) {
	t.Logf("oc login")
	cmd := cluster.clusterCmd(nil, "oc", "login", "--certificate-authority=openshift.local.config/master/ca.crt", "-u", "myuser", "-p", "mypw", "https://localhost:8443")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Regexp(t, "(?s).*Login successful.*", string(out)) // (?s) : '.' will also match newlines

	outString := combinedOutputOfCommand(t, "oc", "new-project", "myns")
	require.Regexp(t, `(?s).*Now using project "myns".*`, outString) // (?s) : '.' will also match newlines
}

// dockerLogin simulates (docker login) to the cluster, or terminates on failure.
// We do not run (docker login) directly, because that requires a running daemon and a docker package.
func (cluster *openshiftCluster) dockerLogin(t *testing.T) {
	cluster.dockerDir = filepath.Join(homedir.Get(), ".docker")
	err := os.Mkdir(cluster.dockerDir, 0700)
	require.NoError(t, err)

	out := combinedOutputOfCommand(t, "oc", "config", "view", "-o", "json", "-o", "jsonpath={.users[*].user.token}")
	t.Logf("oc config value: %s", out)
	authValue := base64.StdEncoding.EncodeToString([]byte("unused:" + out))
	auths := []string{}
	for _, port := range []int{5000, 5005, 5006} {
		auths = append(auths, fmt.Sprintf(`"localhost:%d": {
				"auth": "%s",
				"email": "unused"
			}`, port, authValue))
	}
	configJSON := `{"auths": {` + strings.Join(auths, ",") + `}}`
	err = os.WriteFile(filepath.Join(cluster.dockerDir, "config.json"), []byte(configJSON), 0600)
	require.NoError(t, err)
}

// relaxImageSignerPermissions opens up the system:image-signer permissions so that
// anyone can work with signatures
// FIXME: This also allows anyone to DoS anyone else; this design is really not all
// that workable, but it is the best we can do for now.
func (cluster *openshiftCluster) relaxImageSignerPermissions(t *testing.T) {
	cmd := cluster.clusterCmd(adminKUBECONFIG, "oadm", "policy", "add-cluster-role-to-group", "system:image-signer", "system:authenticated")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", string(out))
	require.Equal(t, "cluster role \"system:image-signer\" added: \"system:authenticated\"\n", string(out))
}

// tearDown stops the cluster services and deletes (only some!) of the state.
func (cluster *openshiftCluster) tearDown(t *testing.T) {
	for _, process := range slices.Backward(cluster.processes) {
		// It’s undocumented what Kill() returns if the process has terminated,
		// so we couldn’t check just for that. This is running in a container anyway…
		_ = process.Process.Kill()
	}
	if cluster.dockerDir != "" {
		err := os.RemoveAll(cluster.dockerDir)
		require.NoError(t, err)
	}
}
