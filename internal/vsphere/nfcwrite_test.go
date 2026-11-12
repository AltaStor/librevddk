// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package vsphere

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

// The write ticket is asked for as VMware's library asks for it (NET-A003): the NFC service, the VM and the disk's
// device key.
func TestNfcWriteTicketRequest(t *testing.T) {
	body := nfcRandomAccessOpenDiskBody{Req: &nfcRandomAccessOpenDiskRequest{
		This:          types.ManagedObjectReference{Type: "NfcService", Value: nfcServiceVCenter},
		VM:            vmRef("vm-42"),
		DiskDeviceKey: 2000,
	}}
	out, err := xml.Marshal(&body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<NfcRandomAccessOpenDisk xmlns="urn:vim25">`,
		`<_this type="NfcService">` + nfcServiceVCenter + `</_this>`,
		`<vm type="VirtualMachine">vm-42</vm>`,
		`<diskDeviceKey>2000</diskDeviceKey>`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("request %s lacks %s", out, want)
		}
	}
}
