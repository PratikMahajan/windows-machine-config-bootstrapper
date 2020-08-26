#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

SKIP_VM_SETUP=""
VM_CREDS=""

# downloads the oc binary but we are just using kubectl as we dont need oc specific commands, oc binary depends on kubectl
# using only kubectl binary reduces the hassle of moving both oc and kubectl to path. We can use kubectl directly from /tmp
get_kubectl(){
  # Download the kubectl binary only if it is not already available
  # We do not validate the version of kubectl if it is available already
  if type kubectl >/dev/null 2>&1; then
    which kubectl
    return
  fi

  KUBECTL_DIR=/tmp/kubectl
  curl -L -s https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.2.2/openshift-client-linux-4.2.2.tar.gz -o openshift-origin-client-tools.tar.gz \
    && tar -xzf openshift-origin-client-tools.tar.gz \
    && mv kubectl $KUBECTL_DIR \
    && rm -rf ./openshift*

  echo $KUBECTL_DIR
}

# delete_deployment deletes the deployed test pod
delete_deployment(){
  if ! $1 delete -f internal/test/wmcb/deploy/deploy.yaml -n default; then
    echo "no deployment found"
  fi
}

WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
cd "${WMCO_ROOT}"

# If ARTIFACT_DIR is not set, create a temp directory for artifacts
ARTIFACT_DIR=${ARTIFACT_DIR:-}
if [ -z "$ARTIFACT_DIR" ]; then
  ARTIFACT_DIR=`mktemp -d`
  echo "ARTIFACT_DIR is not set. Artifacts will be stored in: $ARTIFACT_DIR"
  export ARTIFACT_DIR=$ARTIFACT_DIR
fi

KUBECTL=$(get_kubectl)

if ! $KUBECTL create secret generic cloud-private-key --from-file=private-key.pem=$KUBE_SSH_KEY_PATH -n default; then
    echo "cloud-private-key already present"
fi
if ! $KUBECTL create secret generic aws-creds --from-file=credentials=$AWS_SHARED_CREDENTIALS_FILE -n default; then
    echo "aws credentials already present"
fi
if ! $KUBECTL apply -f internal/test/wmcb/deploy/role.yaml -n default; then
    echo "role already present"
fi
if ! $KUBECTL apply -f internal/test/wmcb/deploy/rolebinding.yaml -n default; then
    echo "rolebinding already present"
fi

sed -i "s~ARTIFACT_DIR_VALUE~${ARTIFACT_DIR}~g" internal/test/wmcb/deploy/deploy.yaml
sed -i "s~REPLACE_IMAGE~${WMCB_IMAGE}~g" internal/test/wmcb/deploy/deploy.yaml

# deploy the test pod on test cluster
if ! $KUBECTL apply -f internal/test/wmcb/deploy/deploy.yaml -n default; then
    echo "application already deployed"
fi

# Sleep for 1 minute while the deployment is being created.
sleep 1m

# streams the log from test pod running on the test cluster
podName=$("${KUBECTL}" get pods -n default | grep -i windows-machine-config-bootstrapper* | awk '{print $1}')
$KUBECTL logs -f ${podName} -n default

if ! ${KUBECTL} logs -l name=windows-machine-config-bootstrapper -n default --tail=4| grep -w ok ; then
  delete_deployment $KUBECTL
  exit 1
fi

delete_deployment $KUBECTL