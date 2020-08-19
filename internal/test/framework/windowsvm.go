package framework

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	mapi "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/providers"
	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/types"
	"github.com/pkg/sftp"
	core "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// BuildWMCB returns the value of buildWMCB. It can be used by WSU to decide if it should build WMCB before using it
	BuildWMCB() bool
	// SetBuildWMCB sets the value of buildWMCB. Setting buildWMCB to true would indicate WSU will build WMCB instead of
	// downloading the latest as per the cluster version. False by default
	SetBuildWMCB(bool)
	// Compose the Windows VM we have from WNI
	types.WindowsVM
}

//createMachineSet() gets the generated MachineSet configuration from cloudprovider package and creates a MachineSet
func (f *TestFramework) createMachineSet() error {
	cloudProvider, err := providers.NewCloudProvider(sshKey)
	if err != nil {
		return fmt.Errorf("error instantiating cloud provider %v", err)
	}
	machineSet, err := cloudProvider.GenerateMachineSet(true, 1)
	if err != nil {
		return fmt.Errorf("error creating Windows MachineSet: %v", err)
	}
	f.machineSet = machineSet
	log.Print("Creating Machine Sets")
	err = f.client.Create(context.TODO(), machineSet)
	if err != nil {
		return fmt.Errorf("unable to create MachineSet %v", err)
	}
	log.Print("Created Machine Sets")
	return nil
}

// waitForMachines() waits until all the machines required are in Provisioned state. It returns an array of all
// the machines created. All the machines are created concurrently.
func (f *TestFramework) waitForMachines(vmCount int) ([]mapi.Machine, error) {
	log.Print("Waiting for Machine Sets ")
	windowsOSLabel := "machine.openshift.io/os-id"
	var provisionedMachines []mapi.Machine
	// it takes approximately 12 minutes in the CI for all the machines to appear.
	timeOut := 12 * time.Minute
	startTime := time.Now()
	for i := 0; time.Since(startTime) <= timeOut; i++ {
		allMachines := &mapi.MachineList{}

		err := f.client.List(context.TODO(), allMachines, client.InNamespace("openshift-machine-api"), client.HasLabels{windowsOSLabel})
		if err != nil {
			return nil, fmt.Errorf("failed to list machines: %v", err)
		}
		provisionedMachines = []mapi.Machine{}

		phaseProvisioned := "Provisioned"

		for _, machine := range allMachines.Items {
			instanceStatus := machine.Status
			if instanceStatus.Phase == nil {
				continue
			}
			instancePhase := *instanceStatus.Phase
			if instancePhase == phaseProvisioned {
				provisionedMachines = append(provisionedMachines, machine)
			}
		}
		time.Sleep(5 * time.Second)
	}
	if vmCount == len(provisionedMachines) {
		return provisionedMachines, nil
	}
	return nil, fmt.Errorf("expected event count %d but got %d", vmCount, len(provisionedMachines))
}

// newWindowsVM creates and sets up a Windows VM in the cloud and returns the WindowsVM interface that can be used to
// interact with the VM. If no error is returned then it is guaranteed that the VM was
// created and can be interacted with.
func (f *TestFramework) newWindowsVM(vmCount int) ([]TestWindowsVM, error) {
	w := make([]TestWindowsVM, vmCount)
	err := f.createMachineSet()
	if err != nil {
		return nil, fmt.Errorf("unable to create Windows MachineSet: %v", err)
	}

	provisionedMachines, err := f.waitForMachines(vmCount)
	if err != nil {
		return nil, err
	}

	for i, machine := range provisionedMachines {
		winVM := &testWindowsVM{
			Windows: &types.Windows{},
		}

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
		creds := types.NewCredentials(instanceID, ipAddress, types.Username)
		winVM.Credentials = creds
		log.Print("setting up ssh")
		winVM.Credentials.SetSSHKey(f.Signer)
		if err := winVM.GetSSHClient(); err != nil {
			return nil, fmt.Errorf("unable to get ssh client for vm %s : %v", instanceID, err)
		}
		w[i] = winVM
	}
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

// Destroy() deletes the MachineSet which in turn deletes all the Machines created by the MachineSet
func (f *TestFramework) Destroy() error {
	log.Print("Destroying MachineSets")
	err := f.client.Delete(context.TODO(), f.machineSet)
	if err != nil {
		return fmt.Errorf("did not create ms %v", err)
	}
	log.Print("MachineSets Destroyed")
	return nil
}

func (w *testWindowsVM) BuildWMCB() bool {
	return w.buildWMCB
}

func (w *testWindowsVM) SetBuildWMCB(buildWMCB bool) {
	w.buildWMCB = buildWMCB
}
