// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package nfc

import (
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"
)

// sentFrames runs request on a session over a pipe and returns what the client wrote, one entry per write call.
// The pipe hands each write to one read, so the entries show where the client split its frames.
func sentFrames(t *testing.T, writes int, request func(c *Conn)) []string {
	t.Helper()
	client, host := net.Pipe()
	c := &Conn{c: client, timeout: 5 * time.Second}
	c.start(4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		request(c)
	}()
	var out []string
	buf := make([]byte, 1<<20)
	_ = host.SetReadDeadline(time.Now().Add(5 * time.Second))
	for len(out) < writes {
		n, err := host.Read(buf)
		if err != nil {
			t.Fatalf("after %d writes: %v", len(out), err)
		}
		out = append(out, hex.EncodeToString(buf[:n]))
	}
	host.Close() // the request fails without a reply; only what it sent matters here
	<-done
	c.Close()
	return out
}

// TestReadRequestFrames checks that every request of the read path puts the same bytes on the wire, split into the
// same writes, as the sender before it took a function for the frames. The expected frames were recorded with that
// sender: the header and payload in one write, then the path or key that follows them in another.
func TestReadRequestFrames(t *testing.T) {
	const handle = 7
	cases := []struct {
		name    string
		writes  int
		request func(c *Conn)
		want    []string
	}{
		{"open", 2, func(c *Conn) { c.OpenDisk("[ds1] vm/vm.vmdk") }, []string{
			"7ada00a104000000" + "3c000000" + "00000000" +
				"10000000" + strings.Repeat("00", 12) + "02000000" + "0e800000" + strings.Repeat("00", 36),
			hex.EncodeToString([]byte("[ds1] vm/vm.vmdk")),
		}},
		{"descriptor key", 2, func(c *Conn) { c.DDBGet(handle, "longContentID") }, []string{
			"7ada00a10b000000" + "10000000" + "00000000" +
				"0700000000000000" + "0d000000" + "00000000",
			hex.EncodeToString([]byte("longContentID")),
		}},
		{"allocation", 1, func(c *Conn) { c.Allocation(handle, 65536, 1<<20, 16) }, []string{
			"7ada00a10d000000" + "30000000" + "00000000" +
				"0700000000000000" + "0000000000000000" + "0000010000000000" + "0000100000000000" +
				"1000000000000000" + "0000000000000000",
		}},
		{"read", 1, func(c *Conn) { c.ReadStart(handle, 4096, make([]byte, 8192)) }, []string{
			"7ada00a107000000" + "2c000000" + "00000000" +
				"0700000000000000" + "01000000" + "00000000" + "0010000000000000" +
				"00200000" + "00000000" + "00200000" + "00200000" + "00000000",
		}},
		{"read without zeros", 1, func(c *Conn) {
			c.SkipZeros = true
			c.ReadStart(handle, 4096, make([]byte, 8192))
		}, []string{
			"7ada00a107000000" + "2c000000" + "00000000" +
				"0700000000000000" + "01000000" + "03000000" + "0010000000000000" +
				"00200000" + "00000000" + "00200000" + "00200000" + "00000000",
		}},
		{"close", 1, func(c *Conn) { c.CloseDisk(handle) }, []string{
			"7ada00a105000000" + "08000000" + "00000000" + "0700000000000000",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sentFrames(t, tc.writes, tc.request)
			if len(got) != len(tc.want) {
				t.Fatalf("%d writes, want %d: %q", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("write %d:\n got %s\nwant %s", i, got[i], tc.want[i])
				}
			}
		})
	}
}
