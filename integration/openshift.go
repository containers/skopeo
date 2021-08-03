package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/pkg/homedir"
	"github.com/go-check/check"
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
func startOpenshiftCluster(c *check.C) *openshiftCluster {
	cluster := &openshiftCluster{}

	dir, err := ioutil.TempDir("", "openshift-cluster")
	c.Assert(err, check.IsNil)
	cluster.workingDir = dir

	cluster.startMaster(c)
	cluster.prepareRegistryConfig(c)
	cluster.startRegistry(c)
	cluster.ocLoginToProject(c)
	cluster.dockerLogin(c)
	cluster.relaxImageSignerPermissions(c)

	return cluster
}

// clusterCmd creates an exec.Cmd in cluster.workingDir with current environment modified by environment
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
func (cluster *openshiftCluster) startMaster(c *check.C) {
	cmd := cluster.clusterCmd(nil, "openshift", "start", "master")
	cluster.processes = append(cluster.processes, cmd)
	stdout, err := cmd.StdoutPipe()
	c.Assert(err, check.IsNil)
	// Send both to the same pipe. This might cause the two streams to be mixed up,
	// but logging actually goes only to stderr - this primarily ensure we log any
	// unexpected output to stdout.
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	c.Assert(err, check.IsNil)

	portOpen, terminatePortCheck := newPortChecker(c, 8443)
	defer func() {
		c.Logf("Terminating port check")
		terminatePortCheck <- true
	}()

	terminateLogCheck := make(chan bool, 1)
	logCheckFound := make(chan bool)
	go func() {
		defer func() {
			c.Logf("Log checker exiting")
		}()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			c.Logf("Log line: %s", line)
			if strings.Contains(line, "Started Origin Controllers") {
				logCheckFound <- true
				return
				// FIXME? We stop reading from stdout; could this block the master?
			}
			// Note: we can block before we get here.
			select {
			case <-terminateLogCheck:
				c.Logf("terminated")
				return
			default:
				// Do not block here and read the next line.
			}
		}
		logCheckFound <- false
	}()
	defer func() {
		c.Logf("Terminating log check")
		terminateLogCheck <- true
	}()

	gotPortCheck := false
	gotLogCheck := false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for !gotPortCheck || !gotLogCheck {
		c.Logf("Waiting for master")
		select {
		case <-portOpen:
			c.Logf("port check done")
			gotPortCheck = true
		case found := <-logCheckFound:
			c.Logf("log check done, found: %t", found)
			if !found {
				c.Fatal("log check done, success message not found")
			}
			gotLogCheck = true
		case <-ctx.Done():
			c.Fatalf("Timed out waiting for master: %v", ctx.Err())
		}
	}
	c.Logf("OK, master started!")
}

// prepareRegistryConfig creates a registry service account and a related k8s client configuration in ${cluster.workingDir}/openshift.local.registry.
func (cluster *openshiftCluster) prepareRegistryConfig(c *check.C) {
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
	runExecCmdWithInput(c, cmd, saJSON)

	cmd = cluster.clusterCmd(adminKUBECONFIG, "oadm", "policy", "add-cluster-role-to-user", "system:registry", "-z", "registry")
	out, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil, check.Commentf("%s", string(out)))
	c.Assert(string(out), check.Equals, "cluster role \"system:registry\" added: \"registry\"\n")

	cmd = cluster.clusterCmd(adminKUBECONFIG, "oadm", "create-api-client-config", "--client-dir=openshift.local.registry", "--basename=openshift-registry", "--user=system:serviceaccount:default:registry")
	out, err = cmd.CombinedOutput()
	c.Assert(err, check.IsNil, check.Commentf("%s", string(out)))
	c.Assert(string(out), check.Equals, "")
}

// startRegistry starts the OpenShift registry with configPart on port, waits for it to be ready, and returns the process object, or terminates on failure.
func (cluster *openshiftCluster) startRegistryProcess(c *check.C, port int, configPath string) *exec.Cmd {
	cmd := cluster.clusterCmd(map[string]string{
		"KUBECONFIG":          "openshift.local.registry/openshift-registry.kubeconfig",
		"DOCKER_REGISTRY_URL": fmt.Sprintf("127.0.0.1:%d", port),
	}, "dockerregistry", configPath)
	consumeAndLogOutputs(c, fmt.Sprintf("registry-%d", port), cmd)
	err := cmd.Start()
	c.Assert(err, check.IsNil)

	portOpen, terminatePortCheck := newPortChecker(c, port)
	defer func() {
		terminatePortCheck <- true
	}()
	c.Logf("Waiting for registry to start")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	select {
	case <-portOpen:
		c.Logf("OK, Registry port open")
	case <-ctx.Done():
		c.Fatalf("Timed out waiting for registry to start: %v", ctx.Err())
	}

	return cmd
}

// startRegistry starts the OpenShift registry and waits for it to be ready, or terminates on failure.
func (cluster *openshiftCluster) startRegistry(c *check.C) {
	// Our “primary” registry
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(c, 5000, "/atomic-registry-config.yml"))

	// A registry configured with acceptschema2:false
	schema1Config := fileFromFixture(c, "/atomic-registry-config.yml", map[string]string{
		"addr: :5000":              "addr: :5005",
		"rootdirectory: /registry": "rootdirectory: /registry-schema1",
		// The default configuration currently already contains acceptschema2: false
	})
	// Make sure the configuration contains "acceptschema2: false", because eventually it will be enabled upstream and this function will need to be updated.
	configContents, err := ioutil.ReadFile(schema1Config)
	c.Assert(err, check.IsNil)
	c.Assert(string(configContents), check.Matches, "(?s).*acceptschema2: false.*")
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(c, 5005, schema1Config))

	// A registry configured with acceptschema2:true
	schema2Config := fileFromFixture(c, "/atomic-registry-config.yml", map[string]string{
		"addr: :5000":              "addr: :5006",
		"rootdirectory: /registry": "rootdirectory: /registry-schema2",
		"acceptschema2: false":     "acceptschema2: true",
	})
	cluster.processes = append(cluster.processes, cluster.startRegistryProcess(c, 5006, schema2Config))
}

// ocLogin runs (oc login) and (oc new-project) on the cluster, or terminates on failure.
func (cluster *openshiftCluster) ocLoginToProject(c *check.C) {
	c.Logf("oc login")
	cmd := cluster.clusterCmd(nil, "oc", "login", "--certificate-authority=openshift.local.config/master/ca.crt", "-u", "myuser", "-p", "mypw", "https://localhost:8443")
	out, err := cmd.CombinedOutput()
	c.Logf("===oc login\n%s\n===oc login===", out)
	c.Assert(err, check.IsNil, check.Commentf("%s", out))
	c.Assert(string(out), check.Matches, "(?s).*Login successful.*") // (?s) : '.' will also match newlines

	out2 := combinedOutputOfCommand(c, "bash", "-c", "find . | sort")
	c.Logf("==FIND .\n%s\n==FIND .", out2)
	out2 = combinedOutputOfCommand(c, "bash", "-c", "echo $HOME; find $HOME | sort")
	c.Logf("==FIND $HOME\n%s\n==FIND $HOME", out2)
	out2 = combinedOutputOfCommand(c, "bash", "-c", "echo ~/.kube/config; cat ~/.kube/config")
	c.Logf("==KUBECONFIG\n%s\n==KUBECONFIG", out2)

	outString := combinedOutputOfCommand(c, "oc", "new-project", "myns")
	c.Assert(outString, check.Matches, `(?s).*Now using project "myns".*`) // (?s) : '.' will also match newlines
}

// dockerLogin simulates (docker login) to the cluster, or terminates on failure.
// We do not run (docker login) directly, because that requires a running daemon and a docker package.
func (cluster *openshiftCluster) dockerLogin(c *check.C) {
	cluster.dockerDir = filepath.Join(homedir.Get(), ".docker")
	err := os.Mkdir(cluster.dockerDir, 0700)
	c.Assert(err, check.IsNil)

	out := combinedOutputOfCommand(c, "oc", "config", "view", "-o", "json", "-o", "jsonpath={.users[*].user.token}")
	c.Logf("oc config value: %s", out)
	authValue := base64.StdEncoding.EncodeToString([]byte("unused:" + out))
	auths := []string{}
	for _, port := range []int{5000, 5005, 5006} {
		auths = append(auths, fmt.Sprintf(`"localhost:%d": {
				"auth": "%s",
				"email": "unused"
			}`, port, authValue))
	}
	configJSON := `{"auths": {` + strings.Join(auths, ",") + `}}`
	err = ioutil.WriteFile(filepath.Join(cluster.dockerDir, "config.json"), []byte(configJSON), 0600)
	c.Assert(err, check.IsNil)
}

// relaxImageSignerPermissions opens up the system:image-signer permissions so that
// anyone can work with signatures
// FIXME: This also allows anyone to DoS anyone else; this design is really not all
// that workable, but it is the best we can do for now.
func (cluster *openshiftCluster) relaxImageSignerPermissions(c *check.C) {
	cmd := cluster.clusterCmd(adminKUBECONFIG, "oadm", "policy", "add-cluster-role-to-group", "system:image-signer", "system:authenticated")
	out, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil, check.Commentf("%s", string(out)))
	c.Assert(string(out), check.Equals, "cluster role \"system:image-signer\" added: \"system:authenticated\"\n")
}

// tearDown stops the cluster services and deletes (only some!) of the state.
func (cluster *openshiftCluster) tearDown(c *check.C) {
	for i := len(cluster.processes) - 1; i >= 0; i-- {
		cluster.processes[i].Process.Kill()
	}
	if cluster.workingDir != "" {
		os.RemoveAll(cluster.workingDir)
	}
	if cluster.dockerDir != "" {
		os.RemoveAll(cluster.dockerDir)
	}
}
