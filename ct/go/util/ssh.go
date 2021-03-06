package util

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/util"
	"golang.org/x/crypto/ssh"
)

const (
	KEY_FILE           = "id_rsa"
	WORKER_NUM_KEYWORD = "{{worker_num}}"
)

type workerResp struct {
	hostname string
	output   string
}

func executeCmd(cmd, hostname string, config *ssh.ClientConfig, timeout time.Duration) (string, error) {
	// Dial up TCP connection to remote machine.
	conn, err := net.Dial("tcp", hostname+":22")
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s. Make sure \"PubkeyAuthentication yes\" is in your sshd_config: %s", hostname, err)
	}
	defer util.Close(conn)
	util.LogErr(conn.SetDeadline(time.Now().Add(timeout)))

	// Create new SSH client connection.
	sshConn, sshChan, req, err := ssh.NewClientConn(conn, hostname+":22", config)
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s: %s", hostname, err)
	}
	// Use client connection to create new client.
	client := ssh.NewClient(sshConn, sshChan, req)

	// Client connections can support multiple interactive sessions.
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s: %s", hostname, err)
	}

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("Errored or Timeout out while running \"%s\" on %s: %s", cmd, hostname, err)
	}
	return stdoutBuf.String(), nil
}

func getKeyFile() (key ssh.Signer, err error) {
	usr, _ := user.Current()
	file := usr.HomeDir + "/.ssh/" + KEY_FILE
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}
	key, err = ssh.ParsePrivateKey(buf)
	if err != nil {
		return
	}
	return
}

// SshToBareMetalMachines connects to the specified workers and runs the specified
// command. If the command does not complete in the given duration then all
// remaining workers are considered timed out. SSH also automatically substitutes
// the sequential number of the worker for the WORKER_NUM_KEYWORD since it is a
// common use case.
func SshToBareMetalMachines(cmd string, workers []string, timeout time.Duration) (map[string]string, error) {
	sklog.Infof("Running \"%s\" on %s with timeout of %s", cmd, workers, timeout)
	numWorkers := len(workers)

	// Ensure that the key file exists.
	key, err := getKeyFile()
	if err != nil {
		return nil, fmt.Errorf("Failed to get key file: %s", err)
	}

	// Initialize the structure with the configuration for ssh.
	config := &ssh.ClientConfig{
		User: CtUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
	}

	var wg sync.WaitGroup
	// m protects workersWithOutputs and remainingWorkers
	var m sync.Mutex
	// Will be populated and returned by this function.
	workersWithOutputs := map[string]string{}
	// Keeps track of which workers are still pending.
	remainingWorkers := map[string]int{}

	// Kick off a goroutine on all workers.
	for i, hostname := range workers {
		wg.Add(1)
		m.Lock()
		remainingWorkers[hostname] = 1
		m.Unlock()
		go func(index int, hostname string) {
			defer wg.Done()
			updatedCmd := strings.Replace(cmd, WORKER_NUM_KEYWORD, strconv.Itoa(index+1), -1)
			output, err := executeCmd(updatedCmd, hostname, config, timeout)
			if err != nil {
				sklog.Errorf("Could not execute ssh cmd: %s", err)
			}
			m.Lock()
			defer m.Unlock()
			workersWithOutputs[hostname] = output
			delete(remainingWorkers, hostname)
			sklog.Infoln()
			sklog.Infof("[%d/%d] Worker %s has completed execution", numWorkers-len(remainingWorkers), numWorkers, hostname)
			sklog.Infof("Remaining workers: %v", remainingWorkers)
		}(i, hostname)
	}

	wg.Wait()
	sklog.Infoln()
	sklog.Infof("Finished running \"%s\" on all %d workers", cmd, numWorkers)
	sklog.Info("========================================")

	m.Lock()
	defer m.Unlock()
	return workersWithOutputs, nil
}
