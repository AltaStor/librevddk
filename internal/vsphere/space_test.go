// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package vsphere

import (
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

// The room a full copy may still need: the capacity not yet used, plus one eighth for a delta's metadata (D-A004).
func TestRoomNeeded(t *testing.T) {
	for _, c := range []struct {
		capacity, used int64
		delta          bool
		want           int64
	}{
		{1 << 30, 0, false, 1 << 30},
		{1 << 30, 1 << 29, false, 1 << 29},
		{1 << 30, 1 << 30, false, 0},
		{1 << 30, 2 << 30, false, 0}, // a file larger than its capacity (metadata) needs nothing more
		{8 << 20, 0, true, 9 << 20},
		{10 << 30, 1 << 20, true, (10<<30 - 1<<20) * 9 / 8},
	} {
		if got := RoomNeeded(c.capacity, c.used, c.delta); got != c.want {
			t.Errorf("RoomNeeded(%d, %d, %v) = %d, want %d", c.capacity, c.used, c.delta, got, c.want)
		}
	}
}

// A delta is known by its backing or by its chain: vCenter reports a VMFS 6 delta with a flat backing.
func TestIsDelta(t *testing.T) {
	var base, sesparse, child DiskDetails
	base.BackingKind, base.Links = "flat", 1
	sesparse.BackingKind, sesparse.Links = "sesparse", 2
	child.BackingKind, child.Links = "flat", 2
	if IsDelta(base) || !IsDelta(sesparse) || !IsDelta(child) {
		t.Errorf("IsDelta: base %v, sesparse %v, flat with a parent %v", IsDelta(base), IsDelta(sesparse), IsDelta(child))
	}
}

// The used space is the top unit of the disk's chain: its descriptor and its extent, not its parents.
func TestTopFileBytes(t *testing.T) {
	l := &types.VirtualMachineFileLayoutEx{
		File: []types.VirtualMachineFileLayoutExFileInfo{
			{Key: 1, Name: "[ds] vm/vm.vmdk", Size: 619},
			{Key: 2, Name: "[ds] vm/vm-flat.vmdk", Size: 2144337920},
			{Key: 3, Name: "[ds] vm/vm-000001.vmdk", Size: 391},
			{Key: 4, Name: "[ds] vm/vm-000001-sesparse.vmdk", Size: 75497472},
			{Key: 5, Name: "[ds] vm/other.vmdk", Size: 500},
		},
		Disk: []types.VirtualMachineFileLayoutExDiskLayout{
			{Key: 2001, Chain: []types.VirtualMachineFileLayoutExDiskUnit{{FileKey: []int32{5}}}},
			{Key: 2000, Chain: []types.VirtualMachineFileLayoutExDiskUnit{{FileKey: []int32{1, 2}}, {FileKey: []int32{3, 4}}}},
		},
	}
	if got, err := topFileBytes(l, 2000); err != nil || got != 391+75497472 {
		t.Fatalf("top of disk 2000: %d, %v", got, err)
	}
	if got, err := topFileBytes(l, 2001); err != nil || got != 500 {
		t.Fatalf("disk 2001: %d, %v", got, err)
	}
	if _, err := topFileBytes(l, 2002); err == nil {
		t.Error("a disk missing from the layout was measured")
	}
	l.Disk[1].Chain[1].FileKey = append(l.Disk[1].Chain[1].FileKey, 9)
	if _, err := topFileBytes(l, 2000); err == nil {
		t.Error("a file missing from the layout was measured")
	}
	if _, err := topFileBytes(nil, 2000); err == nil {
		t.Error("no layout was measured")
	}
}
