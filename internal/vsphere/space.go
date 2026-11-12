// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package vsphere

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// Space is what a write over HotAdd may still add to a disk and what its datastore has free (D-A004).
type Space struct {
	Used      int64  // bytes the disk's top file uses on the datastore
	Need      int64  // bytes the top file may still grow by (RoomNeeded)
	Free      int64  // the datastore's free bytes
	Datastore string // the datastore's name
	// Cached names the values read without a refresh because vCenter refused it, with the reason.
	Cached []string
}

// IsDelta reports whether a disk's file is a delta: a sparse or SE sparse file, or any file with a parent. vCenter
// reports a VMFS 6 delta with the backing type of a flat disk, so the chain decides (HOTADD-A005).
func IsDelta(det DiskDetails) bool {
	return det.BackingKind == "sparse" || det.BackingKind == "sesparse" || det.Links > 1
}

// RoomNeeded is the most a disk's top file can still grow by a full copy: the capacity not yet used, and for a
// delta one eighth more for its own metadata, which took about one ninth of the data written in HOTADD-A005.
func RoomNeeded(capacity, used int64, delta bool) int64 {
	need := max(capacity-used, 0)
	if delta {
		need += need / 8
	}
	return need
}

// WriteSpace reads the space the top file of the disk det uses, from the VM's file layout after
// RefreshStorageInfo, and its datastore's free space after RefreshDatastore. vCenter keeps both values stale
// without a refresh (HOTADD-A003, HOTADD-A005). A refresh that is refused leaves the cached value, named in
// Space.Cached.
func (s *Session) WriteSpace(ctx context.Context, det DiskDetails) (Space, error) {
	var sp Space
	ref := vmRef(det.VM)
	if err := object.NewVirtualMachine(s.c, ref).RefreshStorageInfo(ctx); err != nil {
		sp.Cached = append(sp.Cached, "the disk's used space ("+withPermission(err, "RefreshStorageInfo").Error()+")")
	}
	var v mo.VirtualMachine
	pc := property.DefaultCollector(s.c)
	if err := pc.RetrieveOne(ctx, ref, []string{"layoutEx"}, &v); err != nil {
		return sp, fmt.Errorf("the VM's file layout: %w", withPermission(err, "layoutEx"))
	}
	used, err := topFileBytes(v.LayoutEx, det.Key)
	if err != nil {
		return sp, err
	}
	sp.Used = used
	sp.Need = RoomNeeded(det.CapacityBytes, used, IsDelta(det))
	ds := types.ManagedObjectReference{Type: "Datastore", Value: det.Datastore}
	if _, err := methods.RefreshDatastore(ctx, s.c, &types.RefreshDatastore{This: ds}); err != nil {
		sp.Cached = append(sp.Cached, "the datastore's free space ("+withPermission(err, "RefreshDatastore").Error()+")")
	}
	var d mo.Datastore
	if err := pc.RetrieveOne(ctx, ds, []string{"summary.name", "summary.freeSpace"}, &d); err != nil {
		return sp, fmt.Errorf("the datastore's free space: %w", withPermission(err, "Datastore summary"))
	}
	sp.Datastore, sp.Free = d.Summary.Name, d.Summary.FreeSpace
	return sp, nil
}

// topFileBytes sums the files of the top unit of the disk with device key key in a file layout: the descriptor
// and the extent of the file that a write changes.
func topFileBytes(l *types.VirtualMachineFileLayoutEx, key int32) (int64, error) {
	if l == nil {
		return 0, fmt.Errorf("vCenter gave no file layout for the VM")
	}
	size := map[int32]int64{}
	for _, f := range l.File {
		size[f.Key] = f.Size
	}
	for _, d := range l.Disk {
		if d.Key != key || len(d.Chain) == 0 {
			continue
		}
		var total int64
		for _, k := range d.Chain[len(d.Chain)-1].FileKey {
			s, ok := size[k]
			if !ok {
				return 0, fmt.Errorf("the VM's file layout names file %d of disk %d but does not list it", k, key)
			}
			total += s
		}
		return total, nil
	}
	return 0, fmt.Errorf("the VM's file layout has no disk with key %d", key)
}
