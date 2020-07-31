module github.com/openshift/windows-machine-config-bootstrapper/internal/test

go 1.12

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200205145930-e9d93e317dd1 // OpenShift 4.3
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20191125132246-f6563a70e19a // OpenShift 4.3
	k8s.io/api => k8s.io/api v0.16.7
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.7
	k8s.io/client-go => k8s.io/client-go v0.16.7
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200520125206-5e266b553d8e // This is coming from machine-api repo
)

require (
	github.com/golang/protobuf v1.3.3 // indirect
	github.com/google/go-github/v29 v29.0.2
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/masterzen/winrm v0.0.0-20200615185753-c42b5136ff88
	github.com/openshift/client-go v0.0.0-20200422192633-6f6c07fc2a70
	github.com/openshift/windows-machine-config-operator v0.0.0-20200723182539-9de618aa7d27
	github.com/pkg/sftp v1.11.0
	github.com/stretchr/testify v1.5.1
	golang.org/x/crypto v0.0.0-20200414173820-0848c9571904
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v12.0.0+incompatible
)
