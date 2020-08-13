module github.com/openshift/windows-machine-config-bootstrapper/internal/test

go 1.14

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200422081840-fdd1b0c14c88 // OpenShift 4.5
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200422192633-6f6c07fc2a70 // OpenShift 4.5
	k8s.io/api => k8s.io/api v0.18.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.2
	k8s.io/client-go => k8s.io/client-go v0.18.2
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200520125206-5e266b553d8e // This is coming from machine-api repo
)

require (
	github.com/aws/aws-sdk-go v1.23.2
	github.com/google/go-github/v29 v29.0.2
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/masterzen/winrm v0.0.0-20190308153735-1d17eaf15943
	github.com/openshift/api v0.0.0-20200424083944-0422dc17083e
	github.com/openshift/client-go v0.0.0-20200422192633-6f6c07fc2a70
	github.com/openshift/machine-api-operator v0.2.1-0.20200520080344-fe76daf636f4
	github.com/pkg/errors v0.8.1
	github.com/pkg/sftp v1.11.0
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20200220183623-bac4c82f6975
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.6.2
)
