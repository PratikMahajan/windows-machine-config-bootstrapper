package types

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterzen/winrm"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// This package should have the types that will be used by component. For example, aws should have it's own
// sub-package
// TODO: Move every cloud provider types here

const (
	// ContainerLogsPort number will be opened on the windows node via firewall
	ContainerLogsPort = "10250"
	// FirewallRuleName is the firewall rule name to open the Container Logs Port
	FirewallRuleName = "ContainerLogsPort"
	// remotePowerShellCmdPrefix holds the PowerShell prefix that needs to be prefixed  for every remote PowerShell
	// command executed on the remote Windows VM
	remotePowerShellCmdPrefix = "powershell.exe -NonInteractive -ExecutionPolicy Bypass "
	// winRMPort is port used for WinRM communication
	winRMPort = 5986
	// SshPort is the SSH port
	SshPort = 22
	// awsUsername is the default windows username on AWS
	Username = "Administrator"
)

// Windows represents a Windows host.
// TODO: Add a struct called Connectivity which has information related to Winrm, SSH and have
//		getters and setters for it so that it can be exposed as a public method
//		jira: https://issues.redhat.com/browse/WINC-239. Remove the fields related to SSHClient and WinrmClient and put
// 		them in connectivity struct
type Windows struct {
	// Creds is used for parsing the vmCreds command line argument
	Credentials *Credentials
	// SSHClient contains the ssh client information to access the Windows VM via ssh
	SSHClient *ssh.Client
	// WinrmClient to access the Windows VM created
	WinrmClient *winrm.Client
}

// WindowsVM is the interface for interacting with a Windows object created by the cloud provider
type WindowsVM interface {
	// CopyFile copies the given file to the remote directory in the Windows VM. The remote directory is created if it
	// does not exist
	CopyFile(string, string) error
	// Run executes the given command remotely on the Windows VM and returns the output of stdout and stderr. If the
	// bool is set, it implies that the cmd is to be execute in PowerShell.
	Run(string, bool) (string, string, error)
	// Run executes the given command remotely on the Windows VM over a ssh connection and returns the combined output
	// of stdout and stderr. If the bool is set, it implies that the cmd is to be execute in PowerShell. This function
	// should be used in scenarios where you want to execute a command that runs in the background. In these cases we
	// have observed that Run() returns before the command completes and as a result killing the process.
	RunOverSSH(string, bool) (string, error)
	// GetCredentials returns the interface for accessing the VM credentials. It is up to the caller to check if non-nil
	// Credentials are returned before usage.
	GetCredentials() *Credentials
	// Reinitialize re-initializes the Windows VM. Presently only the ssh client is reinitialized.
	Reinitialize() error
}

func (w *Windows) CopyFile(filePath, remoteDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("CopyFile cannot be called without a SSH client")
	}

	ftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp client initialization failed: %v", err)
	}
	defer ftp.Close()

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening %s file to be transferred: %v", filePath, err)
	}
	defer f.Close()

	if err = ftp.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("error creating remote directory %s: %v", remoteDir, err)
	}

	remoteFile := remoteDir + "\\" + filepath.Base(filePath)
	dstFile, err := ftp.Create(remoteFile)
	if err != nil {
		return fmt.Errorf("error initializing %s file on Windows VMs: %v", remoteFile, err)
	}

	_, err = io.Copy(dstFile, f)
	if err != nil {
		return fmt.Errorf("error copying %s to the Windows VM: %v", filePath, err)
	}

	// Forcefully close it so that we can execute the binary later
	dstFile.Close()
	return nil
}

func (w *Windows) Run(cmd string, psCmd bool) (string, string, error) {
	if w.WinrmClient == nil {
		return "", "", fmt.Errorf("Run cannot be called without a WinRM client")
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	if psCmd {
		cmd = remotePowerShellCmdPrefix + cmd
	}
	// Remotely execute the test binary.
	exitCode, err := w.WinrmClient.Run(cmd, stdout, stderr)
	if err != nil {
		return "", "", fmt.Errorf("error while executing %s remotely: %v", cmd, err)
	}

	if exitCode != 0 {
		return stdout.String(), stderr.String(), fmt.Errorf("%s returned %d exit code", cmd, exitCode)
	}

	return stdout.String(), stderr.String(), nil
}

func (w *Windows) RunOverSSH(cmd string, psCmd bool) (string, error) {
	if w.SSHClient == nil {
		return "", fmt.Errorf("RunOverSSH cannot be called without a ssh client")
	}

	session, err := w.SSHClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	if psCmd {
		cmd = remotePowerShellCmdPrefix + cmd
	}

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (w *Windows) GetCredentials() *Credentials {
	return w.Credentials
}

// GetSSHClient gets the ssh client associated with Windows VM created
func (w *Windows) GetSSHClient() error {
	if w.SSHClient != nil {
		// Close the existing client to be on the safe side
		if err := w.SSHClient.Close(); err != nil {
			log.Printf("warning - error closing ssh client connection: %v", err)
		}
	}

	config := &ssh.ClientConfig{
		User:            w.Credentials.GetUserName(), //TODO: Change this to make sure that this works for Azure.
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(w.Credentials.GetSSHKey())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", w.Credentials.GetIPAddress()+":22", config)
	if err != nil {
		return fmt.Errorf("failed to dial to ssh server: %s", err)
	}
	w.SSHClient = sshClient
	return nil
}

func (w *Windows) Reinitialize() error {
	if err := w.GetSSHClient(); err != nil {
		return fmt.Errorf("failed to reinitialize ssh client: %v", err)
	}
	if err := w.SetupWinRMClient(); err != nil {
		return fmt.Errorf("failed to reinitialize WinRM client: %v", err)
	}
	return nil
}

// SetupWinRMClient sets up the winrm client to be used while accessing Windows node
func (w *Windows) SetupWinRMClient() error {
	host := w.Credentials.GetIPAddress()
	user := w.Credentials.GetUserName()
	// Connect to the bootstrapped host. Timeout is high as the Windows Server image is slow to download
	endpoint := winrm.NewEndpoint(host, winRMPort, true, true,
		nil, nil, nil, time.Minute*10)

	params := winrm.DefaultParameters
	params.Dial = w.SSHClient.Dial

	winrmClient, err := winrm.NewClientWithParameters(endpoint, user, "", params)
	if err != nil {
		return fmt.Errorf("failed to set up winrm client with error: %v", err)
	}
	w.WinrmClient = winrmClient
	return nil
}

// Credentials holds the information to access the Windows instance created.
type Credentials struct {
	// instanceID uniquely identifies the instanceID
	instanceID string
	// ipAddress contains the public ip address of the instance created
	ipAddress string
	// sshKey to access the instance created
	sshKey ssh.Signer
	// user used for accessing the  instance created
	user string
}

// NewCredentials takes the instanceID, ip address and user of the Windows instance created and returns the
// Credentials structure
func NewCredentials(instanceID, ipAddress, user string) *Credentials {
	return &Credentials{instanceID: instanceID, ipAddress: ipAddress, user: user}
}

// GetIPAddress returns the ip address of the given node
func (cred *Credentials) GetIPAddress() string {
	return cred.ipAddress
}

// GetSSHKey returns the password associated with the given node
func (cred *Credentials) GetSSHKey() ssh.Signer {
	return cred.sshKey
}

// SetSSHKey sets the ssh signer for given node
func (cred *Credentials) SetSSHKey(signer ssh.Signer) {
	cred.sshKey = signer
}

// GetInstanceID returns the instanceId associated with the given node
func (cred *Credentials) GetInstanceId() string {
	return cred.instanceID
}

// GetUserName returns the user name associated with the given node
func (cred *Credentials) GetUserName() string {
	return cred.user
}

// String returns the string representation of Creds. This is required for Creds to be used with flags.
func (c *Credentials) String() string {
	return fmt.Sprintf("%v", *c)
}

// Set populates the list of credentials from the vmCreds command line argument
func (c *Credentials) Set(value string) error {
	if value == "" {
		return nil
	}

	splitValue := strings.Split(value, ",")
	// Credentials consists of three elements, so this has to be
	if len(splitValue) != 2 {
		return fmt.Errorf("incomplete VM credentials provided")
	}

	// TODO: Add input validation if we want to use this in production
	// TODO: Change username based on cloud provider if this is to be used for clouds other than AWS
	cred := NewCredentials(splitValue[0], splitValue[1], Username)
	*c = *cred

	return nil
}
