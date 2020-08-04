package framework

import (
	"context"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	core "k8s.io/api/core/v1"
	"log"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"strings"
	"time"

	mapi "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/types"
	"github.com/openshift/windows-machine-config-operator/test/e2e/providers"
	"github.com/pkg/sftp"
)

const (
	// sshKey is the key that will be used to access created Windows VMs
	sshKey = "openshift-dev"
)

// cloudProvider holds the information related to cloud provider
// TODO: Move this to proper location which can destroy the VM that got created.
//		https://issues.redhat.com/browse/WINC-245
var cloudProvider providers.CloudProvider

// testWindowsVM holds the information related to the test Windows VM. This should hold the specialized information
// related to test suite.
type testWindowsVM struct {
	*types.Windows
	// buildWMCB indicates if WSU should build WMCB and use it
	// TODO This is a WSU specific property and should be moved to wsu_test -> https://issues.redhat.com/browse/WINC-249
	buildWMCB bool
}

// TestWindowsVM is the interface for interacting with a Windows VM in the test framework. This will hold the
// specialized information related to test suite
type TestWindowsVM interface {
	// RetrieveDirectories recursively copies the files and directories from the directory in the remote Windows VM
	// to the given directory on the local host.
	RetrieveDirectories(string, string) error
	// Destroy destroys the Windows VM
	// TODO: Remove this and move it to framework or other higher level object capable of doing deletions.
	//		jira: https://issues.redhat.com/browse/WINC-243
	Destroy() error
	// BuildWMCB returns the value of buildWMCB. It can be used by WSU to decide if it should build WMCB before using it
	BuildWMCB() bool
	// SetBuildWMCB sets the value of buildWMCB. Setting buildWMCB to true would indicate WSU will build WMCB instead of
	// downloading the latest as per the cluster version. False by default
	SetBuildWMCB(bool)
	// Compose the Windows VM we have from WNI
	types.WindowsVM
}

//
func createMachineSet() error {
	cloudProvider, err := providers.NewCloudProvider(sshKey)
	if err != nil {
		return fmt.Errorf("error instantiating cloud provider %v", err)
	}
	machineSet, err := cloudProvider.GenerateMachineSet(true, 1)
	if err != nil {
		return fmt.Errorf("error creating Windows MachineSet: %v", err)
	}
	cfg, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("did not create ms %v", err)
	}

	k8c, err := client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("did not create ms %v", err)
	}
	log.Print("Creating Machine Sets")
	err = k8c.Create(context.TODO(), machineSet)
	if err != nil {
		log.Print("%v", err)
		return fmt.Errorf("did not create ms %v", err)
	}
	log.Print("Created Machine Sets")

	return nil
}

//
func waitForMachines() ([]mapi.Machine, error) {
	log.Print("Waiting for Machine Sets ")
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	k8c, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}
	windowsOSLabel := "node.openshift.io/os-id"
	var provisionedMachines []mapi.Machine
	timeOut := 2 * time.Minute
	startTime := time.Now()
	requiredVMCount := 1
	for i := 0; time.Since(startTime) <= timeOut; i++ {
		allMachines := &mapi.MachineList{}

		err := k8c.List(context.TODO(), allMachines, client.InNamespace("openshift-machine-api"), client.HasLabels{windowsOSLabel})
		if err != nil {
			return nil, fmt.Errorf("failed to list machines: %v", err)
		}

		provisionedMachines = []mapi.Machine{}

		phaseProvisioned := "Provisioned"

		for machine := range allMachines.Items {
			instanceStatus := allMachines.Items[machine].Status
			if instanceStatus.Phase == nil {
				continue
			}
			instancePhase := *instanceStatus.Phase
			if instancePhase == phaseProvisioned {
				provisionedMachines = append(provisionedMachines, allMachines.Items[machine])
			}
		}
		time.Sleep(5 * time.Second)
	}
	if requiredVMCount == len(provisionedMachines) {
		return provisionedMachines, nil
	}
	return nil, fmt.Errorf("expected event count %d but got %d", requiredVMCount, len(provisionedMachines))
}

// newWindowsVM creates and sets up a Windows VM in the cloud and returns the WindowsVM interface that can be used to
// interact with the VM. If credentials are passed then it is assumed that VM already exists in the cloud and those
// credentials will be used to interact with the VM. If no error is returned then it is guaranteed that the VM was
// created and can be interacted with. If skipSetup is true, then configuration steps are skipped.
func newWindowsVM(vmCount int, credentials *types.Credentials, sshKey ssh.Signer) ([]TestWindowsVM, error) {
	w := make([]TestWindowsVM, vmCount)
	err := createMachineSet()
	if err != nil {
		return nil, fmt.Errorf("unable to create Windows MachineSet: %v", err)
	}

	provisionedMachines, err := waitForMachines()
	if err != nil {
		return nil, err
	}

	log.Print("%v", provisionedMachines)
	for i, machine := range provisionedMachines {
		winVM := &testWindowsVM{}

		ipAddress := ""
		for _, address := range machine.Status.Addresses {
			if address.Type == core.NodeInternalIP {
				ipAddress = address.Address
			}
		}
		if len(ipAddress) == 0 {
			return nil, fmt.Errorf("no associated internal ip for machine: %s", machine.Name)
		}

		// Get the instance ID associated with the Windows machine.
		providerID := *machine.Spec.ProviderID
		if len(providerID) == 0 {
			return nil, fmt.Errorf("no provider id associated with machine")
		}
		// Ex: aws:///us-east-1e/i-078285fdadccb2eaa. We always want the last entry which is the instanceID
		providerTokens := strings.Split(providerID, "/")
		instanceID := providerTokens[len(providerTokens)-1]
		if len(instanceID) == 0 {
			return nil, fmt.Errorf("empty instance id in provider id")
		}
		winVM.Credentials = types.NewCredentials(instanceID, ipAddress, types.Username)
		winVM.Credentials.SetSSHKey(sshKey)
		if err := winVM.GetSSHClient(); err != nil {
			return nil, fmt.Errorf("unable to get ssh client for vm %s : %v", instanceID, err)
		}
		if err := winVM.SetupWinRMClient(); err != nil {
			return nil, fmt.Errorf("unable to setup winRM client for vm %s : %v", instanceID, err)
		}

		w[i] = winVM
	}

	//if credentials == nil {
	//	// create windows machine set
	//
	//	// wait for machines to enter provisioned state
	//
	//	// TypeAssert to the WindowsVM struct we want
	//	winVM, ok := windowsVM.(*types.Windows)
	//	if !ok {
	//		return nil, fmt.Errorf("error asserting Windows VM: %v", err)
	//	}
	//	w.Windows = winVM
	//} else {
	//	//TODO: Add username as well, as it will change depending on cloud provider
	//	// TODO: get ssh key from userdata secret
	//	if credentials.GetIPAddress() == "" || credentials.GetSSHKey() == "" {
	//		return nil, fmt.Errorf("sshkey or IP address not specified in credentials")
	//	}
	//	w.Windows = &types.Windows{}
	//	w.Credentials = credentials
	//}

	// TODO: Parse the output of the `Get-Service sshd, ssh-agent` on the Windows node to check if the windows nodes
	// has those services present

	return w, nil
}

func (w *testWindowsVM) RetrieveDirectories(remoteDir string, localDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("cannot retrieve remote directory without a ssh client")
	}

	// creating a local directory to store the files and directories from remote directory.
	err := os.MkdirAll(localDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not create %s: %v", localDir, err)
	}

	sftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp initialization failed: %v", err)
	}
	defer sftp.Close()

	// Get the list of all files in the directory
	remoteFiles, err := sftp.ReadDir(remoteDir)
	if err != nil {
		return fmt.Errorf("error opening remote directory %s: %v", remoteDir, err)
	}

	for _, remoteFile := range remoteFiles {
		remotePath := filepath.Join(remoteDir, remoteFile.Name())
		localPath := filepath.Join(localDir, remoteFile.Name())
		// check if it is a directory, call itself again
		if remoteFile.IsDir() {
			if err = w.RetrieveDirectories(remotePath, localPath); err != nil {
				log.Printf("error while retrieving %s directory from Windows : %v", remotePath, err)
			}
		} else {
			// logging errors as a best effort to retrieve files from remote directory
			if err = w.copyFileFrom(sftp, remotePath, localPath); err != nil {
				log.Printf("error while retrieving %s file from Windows : %v", remotePath, err)
			}
		}
	}
	return nil
}

// copyFileFrom copies a file from remote directory to the local directory.
func (w *testWindowsVM) copyFileFrom(sftp *sftp.Client, remotePath, localPath string) error {
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("error creating file locally: %v", err)
	}
	// TODO: Check if there is some performance implication of multiple Open calls.
	remoteFile, err := sftp.Open(remotePath)
	if err != nil {
		return fmt.Errorf("error while opening remote file on the Windows VM: %v", err)
	}
	// logging the errors instead of returning to allow closing of files
	_, err = io.Copy(localFile, remoteFile)
	if err != nil {
		log.Printf("error retrieving file %v from Windows VM: %v", localPath, err)
	}
	// flush memory
	if err = localFile.Sync(); err != nil {
		log.Printf("error flusing memory: %v", err)
	}
	if err := remoteFile.Close(); err != nil {
		log.Printf("error closing file on the remote host %s", localPath)
	}
	if err := localFile.Close(); err != nil {
		log.Printf("error closing file %s locally", localPath)
	}
	return nil
}

func (w *testWindowsVM) Destroy() error {
	// There is no VM to destroy
	if cloudProvider == nil || w.Windows == nil || w.GetCredentials() == nil {
		return nil
	}
	//return cloudProvider.DestroyWindowsVMs()
	return nil
}

func (w *testWindowsVM) BuildWMCB() bool {
	return w.buildWMCB
}

func (w *testWindowsVM) SetBuildWMCB(buildWMCB bool) {
	w.buildWMCB = buildWMCB
}
