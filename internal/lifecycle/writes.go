// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import (
	"fmt"
	"os"
	"strings"

	"github.com/openvddk/openvddk/internal/config"
)

// Writes are off until a switch outside the program turns them on for a transport family (D-A001): its key in the
// configuration file passed to InitEx, or its environment variable. A consumer that opens disks without the read-only
// flag stays read-only on a host where none is set, so a library upgrade never starts writing disks by itself.

// Family is a group of transports that writes are switched on for together.
type Family string

const (
	// HotAdd is HotAdd (D-A003).
	HotAdd Family = "hotadd"
	// NBD is NBDSSL and NBD (D-A002).
	NBD Family = "nbd"
)

// Switch is the key and the environment variable that turn writes on for a family.
type Switch struct {
	Family Family
	Key    string // read from InitEx's configuration file
	Env    string
	Label  string // the transports, for the log
}

// Switches lists every family's switch, in the order they are logged.
var Switches = []Switch{
	{Family: HotAdd, Key: "OpenVDDK.hotadd.write", Env: "OPENVIX_HOTADD_WRITE", Label: "HotAdd"},
	{Family: NBD, Key: "OpenVDDK.nbd.write", Env: "OPENVIX_NBD_WRITE", Label: "NBDSSL and NBD"},
}

// switchFor returns the switch of family f.
func switchFor(f Family) (Switch, bool) {
	for _, sw := range Switches {
		if sw.Family == f {
			return sw, true
		}
	}
	return Switch{}, false
}

// OffReason says how writes over family f are turned on, for the reason of a refused open.
func OffReason(f Family) string {
	sw, _ := switchFor(f)
	return fmt.Sprintf("writes over %s are off: %s = \"true\" in the configuration file passed to InitEx, or %s=1 in the environment, turns them on",
		sw.Label, sw.Key, sw.Env)
}

// writesSwitch decides which families disks may be written over. For each family it returns the switches that
// turned writes on, in the order the environment and cfgName, the file passed to InitEx, and the problems found on
// the way. A value that is not a switch value leaves writes off from that source and is reported.
func writesSwitch(getenv func(string) string, cfg *config.Config, cfgName string) (map[Family][]string, []string) {
	by := map[Family][]string{}
	var problems []string
	for _, sw := range Switches {
		if v := getenv(sw.Env); v != "" {
			on, ok := switchValue(v)
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s=%q is not 1, true, 0 or false; writes over %s stay off from the environment", sw.Env, v, sw.Label))
			case on:
				by[sw.Family] = append(by[sw.Family], sw.Env+"="+v)
			}
		}
		problems = keySwitch(cfg, cfgName, sw, by, problems)
	}
	return by, problems
}

// keySwitch reads the key of sw from cfg, a file named where.
func keySwitch(cfg *config.Config, where string, sw Switch, by map[Family][]string, problems []string) []string {
	v, set := cfg.Get(sw.Key)
	if !set {
		return problems
	}
	on, ok := switchValue(v)
	switch {
	case !ok:
		problems = append(problems, fmt.Sprintf("%s = %q in %s is not true or false; writes over %s stay off from that file", sw.Key, v, where, sw.Label))
	case on:
		by[sw.Family] = append(by[sw.Family], fmt.Sprintf("%s = %q in %s", sw.Key, v, where))
	}
	return problems
}

// switchValue reads a switch: 1, true, yes or on turn it on; 0, false, no or off leave it off; ok is false for any
// other value.
func switchValue(v string) (on, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}
	return false, false
}

// Writes reports whether disks may be opened for writing over family f, and the switches that turned it on.
func (s *State) Writes(f Family) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.writesBy[f]) > 0, strings.Join(s.writesBy[f], ", ")
}

// lookupEnv is os.Getenv; tests replace it.
var lookupEnv = os.Getenv
