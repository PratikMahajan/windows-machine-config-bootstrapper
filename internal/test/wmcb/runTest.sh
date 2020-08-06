#!/bin/bash

# Transfer the files and run the unit and e2e tests
CGO_ENABLED=0 GO111MODULE=on go test -v -run=TestWMCB -filesToBeTransferred="wmcb_unit_test.exe,wmcb_e2e_test.exe,wmcb/powershell/wget-ignore-cert.ps1" -vmCreds="$VM_CREDS" $SKIP_VM_SETUP -timeout=30m .
CGO_ENABLED=0 GO111MODULE=on go test -v -run=TestWMCB -filesToBeTransferred="../wmcb_unit_test.exe,../wmcb_e2e_test.exe,powershell/wget-ignore-cert.ps1" -vmCreds="$VM_CREDS" $SKIP_VM_SETUP -timeout=30m .