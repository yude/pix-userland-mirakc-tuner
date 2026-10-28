// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var base = []string{"--type", "GR", "--channel", "T27", "--duration", "-", "--instance", "dt295"}

func TestArguments(t *testing.T) {
	tests := []struct {
		name, kind, channel string
		extra               []string
		want                []string
	}{
		{"GR numeric", "GR", "27", []string{"--runtime-dir", "/run/pixd", "--group"}, []string{"--instance", "dt295", "--system", "isdb-t", "--channel", "T27", "--time", "-", "--output", "-", "--runtime-dir", "/run/pixd", "--group"}},
		{"BS slot", "BS", "BS01_0", nil, []string{"--instance", "dt295", "--system", "isdb-s", "--channel", "BS01", "--time", "-", "--output", "-", "--slot", "0"}},
		{"BS TSID", "BS", "BS23@16400", nil, []string{"--instance", "dt295", "--system", "isdb-s", "--channel", "BS23", "--time", "-", "--output", "-", "--tsid", "16400"}},
		{"CS slot", "CS", "CS22_7", nil, []string{"--instance", "dt295", "--system", "isdb-s", "--channel", "CS22", "--time", "-", "--output", "-", "--slot", "7"}},
		{"CS explicit TSID", "CS", "CS22", []string{"--tsid", "29024"}, []string{"--instance", "dt295", "--system", "isdb-s", "--channel", "CS22", "--time", "-", "--output", "-", "--tsid", "29024"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--type", tc.kind, "--channel", tc.channel, "--duration", "-", "--instance", "dt295"}
			got, err := parse(append(args, tc.extra...))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestRejectInvalid(t *testing.T) {
	tests := [][]string{
		{"--type", "SKY"}, {"--channel", "T12"}, {"--channel", "T63"},
		{"--channel", "T27;touch /tmp/never"}, {"--duration", "0"}, {"--duration", "1;id"},
		{"--instance", "x;id"}, {"--channel", "BS01_8", "--type", "BS"},
		{"--channel", "BS01", "--type", "BS"}, {"--channel", "CS22", "--type", "CS"},
		{"--channel", "CS22@65535", "--type", "CS"}, {"--channel", "CS22@0", "--type", "CS"},
		{"--channel", "BS02_0", "--type", "BS"},
		{"--channel", "CS22_0", "--type", "CS", "--tsid", "1"},
		{"--group"}, {"--runtime-dir", "relative"}, {"--unexpected", "value"},
		{"--type", "GR", "--type", "GR"},
	}
	for _, changes := range tests {
		args := append([]string(nil), base...)
		if len(changes) == 2 {
			found := false
			for i := 0; i < len(args); i += 2 {
				if args[i] == changes[0] {
					args[i+1], found = changes[1], true
					break
				}
			}
			if !found {
				args = append(args, changes...)
			}
		} else {
			args = append(args, changes...)
		}
		if _, err := parse(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

func TestExecPreservesStreamAndExit(t *testing.T) {
	root := t.TempDir()
	build := func(name, source string) string {
		t.Helper()
		path := filepath.Join(root, name)
		cmd := exec.Command("go", "build", "-o", path, source)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		return path
	}
	adapter := build("pix-mirakc-tuner", ".")
	mock := build("mock-pix-ts", "./testdata/mock_pix_ts.go")
	argsFile := filepath.Join(root, "args.json")
	call := func(mode string) ([]byte, []byte, int) {
		t.Helper()
		cmd := exec.Command(adapter, append(base, "--runtime-dir", "/run/pixd", "--group")...)
		cmd.Env = append(os.Environ(), "PIX_TS_BIN="+mock, "PIX_MOCK_ARGS="+argsFile, "PIX_MOCK_MODE="+mode)
		var out, diagnostics bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostics
		err := cmd.Run()
		if err == nil {
			return out.Bytes(), diagnostics.Bytes(), 0
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return out.Bytes(), diagnostics.Bytes(), exit.ExitCode()
		}
		t.Fatal(err)
		return nil, nil, -1
	}
	output, diagnostics, code := call("stream")
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	if code != 0 || !bytes.Equal(output, bytes.Repeat(packet, 128)) || string(diagnostics) != "synthetic diagnostic\n" {
		t.Fatalf("stream code=%d bytes=%d stderr=%q", code, len(output), diagnostics)
	}
	var args []string
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	want, err := parse(append(base, "--runtime-dir", "/run/pixd", "--group"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("mock received %q; want %q", args, want)
	}
	output, diagnostics, code = call("fail")
	if code != 7 || len(output) != 0 || !strings.Contains(string(diagnostics), "synthetic IPC failure") {
		t.Fatalf("failure code=%d bytes=%d stderr=%q", code, len(output), diagnostics)
	}
}
