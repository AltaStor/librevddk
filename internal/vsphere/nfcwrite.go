// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package vsphere

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/openvddk/openvddk/internal/nfc"
)

// NfcRandomAccessOpenDisk is the ticket VMware's library asks for when it opens a disk for writing over NBD or
// NBDSSL, where a read asks for NfcGetVmFiles (NET-A003): the VM and the disk's device key; the answer is a
// HostServiceTicket.
type nfcRandomAccessOpenDiskRequest struct {
	This          types.ManagedObjectReference `xml:"_this"`
	VM            types.ManagedObjectReference `xml:"vm"`
	DiskDeviceKey int32                        `xml:"diskDeviceKey"`
}

type nfcRandomAccessOpenDiskResponse struct {
	Returnval types.HostServiceTicket `xml:"returnval"`
}

type nfcRandomAccessOpenDiskBody struct {
	Req    *nfcRandomAccessOpenDiskRequest  `xml:"urn:vim25 NfcRandomAccessOpenDisk,omitempty"`
	Res    *nfcRandomAccessOpenDiskResponse `xml:"NfcRandomAccessOpenDiskResponse,omitempty"`
	Fault_ *soap.Fault                      `xml:"http://schemas.xmlsoap.org/soap/envelope/ Fault,omitempty"`
}

func (b *nfcRandomAccessOpenDiskBody) Fault() *soap.Fault { return b.Fault_ }

// NfcWriteTicket asks the server for a one-time NFC ticket that lets the session open the VM's disk with device key
// key for writing, as VMware's library asks for it (NfcRandomAccessOpenDisk, NET-A003). The host refuses a write
// open made with a ticket from NfcGetVmFiles. As with NfcTicket, a ticket without a host names the server the
// session was opened with.
func (s *Session) NfcWriteTicket(ctx context.Context, vm string, key int32) (nfc.Ticket, error) {
	var req, res nfcRandomAccessOpenDiskBody
	req.Req = &nfcRandomAccessOpenDiskRequest{This: s.nfcService(ctx), VM: vmRef(vm), DiskDeviceKey: key}
	if err := s.c.RoundTrip(ctx, &req, &res); err != nil {
		if perr := withPermission(err, "NfcRandomAccessOpenDisk"); perr != err {
			return nfc.Ticket{}, perr
		}
		return nfc.Ticket{}, fmt.Errorf("NfcRandomAccessOpenDisk: %w", err)
	}
	if res.Res == nil {
		return nfc.Ticket{}, errors.New("the server returned no NFC ticket")
	}
	t := res.Res.Returnval
	host := t.Host
	if host == "" {
		host = strings.Trim(s.server, "[]")
	}
	return nfc.Ticket{
		Host:          host,
		Port:          int(t.Port),
		SSLThumbprint: t.SslThumbprint,
		Service:       t.Service,
		SessionID:     t.SessionId,
	}, nil
}
