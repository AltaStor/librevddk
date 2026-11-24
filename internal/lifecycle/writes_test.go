// Copyright 2026 Norvitex OU (AltaStor.com)
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import (
	"reflect"
	"strings"
	"testing"

	"github.com/openvddk/openvddk/internal/config"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// Writes over NBDSSL and NBD are off unless a switch turns them on; each switch is named when it does.
func TestWritesSwitch(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		initEx   string
		by       []string
		problems int
	}{
		{name: "nothing set"},
		{name: "environment", env: map[string]string{"OPENVIX_NBD_WRITE": "1"},
			by: []string{"OPENVIX_NBD_WRITE=1"}},
		{name: "environment set to 0", env: map[string]string{"OPENVIX_NBD_WRITE": "0"}},
		{name: "environment not a switch value", env: map[string]string{"OPENVIX_NBD_WRITE": "maybe"},
			problems: 1},
		{name: "configuration file", initEx: "OpenVix.nbd.write = TRUE\n",
			by: []string{"OpenVDDK.nbd.write = \"TRUE\" in initex.conf"}},
		{name: "configuration file set to false", initEx: "OpenVDDK.nbd.write = false\n"},
		{name: "configuration file not a switch value", initEx: "OpenVDDK.nbd.write = yesplease\n",
			problems: 1},
		{name: "both switches", env: map[string]string{"OPENVIX_NBD_WRITE": "true"},
			initEx: "OpenVDDK.nbd.write = on\n",
			by:     []string{"OPENVIX_NBD_WRITE=true", "OpenVDDK.nbd.write = \"on\" in initex.conf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, problems := writesSwitch(env(c.env), config.Parse([]byte(c.initEx)), "initex.conf")
			by := got[NBD]
			if !reflect.DeepEqual(by, c.by) {
				t.Errorf("turned on by %q, want %q", by, c.by)
			}
			if len(problems) != c.problems {
				t.Errorf("problems %q, want %d", problems, c.problems)
			}
			for _, p := range problems {
				if !strings.Contains(p, "1, true, yes or on; 0, false, no or off") {
					t.Errorf("problem %q does not name every switch value", p)
				}
			}
		})
	}
}

// Init records the switches and Exit forgets them.
func TestInitRecordsWrites(t *testing.T) {
	defer func(e func(string) string) { lookupEnv = e }(lookupEnv)
	lookupEnv = env(nil)
	s := &State{cfg: config.Empty()}
	if code := s.Init(Options{Major: 8}); code != 0 {
		t.Fatalf("Init = %d", code)
	}
	if on, by := s.Writes(NBD); on || by != "" {
		t.Fatalf("writes on by %q with no switch set", by)
	}
	lookupEnv = env(map[string]string{"OPENVIX_NBD_WRITE": "1"})
	s.Init(Options{Major: 8})
	if on, by := s.Writes(NBD); !on || by != "OPENVIX_NBD_WRITE=1" {
		t.Fatalf("writes on=%v by %q", on, by)
	}
	s.Exit()
	if on, _ := s.Writes(NBD); on {
		t.Fatal("writes still on after Exit")
	}
}

// The reason of a refused open names the family's key, the configuration file and the variable.
func TestOffReason(t *testing.T) {
	r := OffReason(NBD)
	for _, want := range []string{"NBDSSL and NBD", "OpenVDDK.nbd.write", "configuration file passed to InitEx", "OPENVIX_NBD_WRITE=1"} {
		if !strings.Contains(r, want) {
			t.Errorf("%q lacks %q", r, want)
		}
	}
}

// Each family has its own switch: one turned on leaves the other off.
func TestWritesSwitchPerFamily(t *testing.T) {
	by, problems := writesSwitch(env(map[string]string{"OPENVIX_HOTADD_WRITE": "1"}),
		config.Parse([]byte("OpenVDDK.nbd.write = false\n")), "initex.conf")
	if len(problems) != 0 || len(by[HotAdd]) != 1 || len(by[NBD]) != 0 {
		t.Fatalf("switches %v, problems %v", by, problems)
	}
	by, _ = writesSwitch(env(nil), config.Parse([]byte("OpenVDDK.hotadd.write = true\nOpenVDDK.nbd.write = true\n")),
		"initex.conf")
	if len(by[HotAdd]) != 1 || len(by[NBD]) != 1 {
		t.Fatalf("switches %v", by)
	}
}
