// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func device(t *testing.T, root, port, vendor, product, serial string) {
	t.Helper()
	dir := filepath.Join(root, port)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"idVendor": vendor, "idProduct": product, "serial": serial} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			root := t.TempDir()
			script := "#!/bin/sh\ntrap 'exit 17' TERM INT\necho $$\nwhile :; do sleep 0.05; done\n"
			if err := os.WriteFile(filepath.Join(root, "pixd"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestExecProcess$")
			command.Env = append(os.Environ(), "PIXD_LAUNCH_TEST_CHILD=1", "PIXD_LAUNCH_TEST_ARGS=--device\nusb:1-2", "PATH="+root+":/usr/bin:/bin")
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill() })
			ready := make(chan string, 1)
			go func() { scanner := bufio.NewScanner(stdout); scanner.Scan(); ready <- scanner.Text() }()
			select {
			case pid := <-ready:
				if pid != strconv.Itoa(command.Process.Pid) {
					t.Fatalf("exec did not preserve PID: %q", pid)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("daemon did not start")
			}
			if err := command.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case err := <-done:
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
					t.Fatalf("signal status: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("daemon did not receive signal")
			}
		})
	}
}

func TestSerialResolution(t *testing.T) {
	root := t.TempDir()
	device(t, root, "1-8.2", "06b8", "103b", "SYNTH-A")
	device(t, root, "2-3", "06b8", "103b", "SYNTH-B")
	device(t, root, "1-9", "ffff", "103b", "SYNTH-A")
	device(t, root, "1-8.2:1.0", "06b8", "103b", "SYNTH-A")
	for selector, want := range map[string]string{
		"serial:SYNTH-A": "usb:1-8.2", "serial:SYNTH-B": "usb:2-3",
		"serial:absent": "serial:absent", "usb:1-8.2": "usb:1-8.2",
		"serial:$(touch bad)": "serial:$(touch bad)",
	} {
		got, err := resolveDevice(root, selector)
		if err != nil || got != want {
			t.Fatalf("%q: got %q, %v; want %q", selector, got, err, want)
		}
	}
	// Replug to a different port: re-evaluate sysfs on every launch.
	if err := os.Rename(filepath.Join(root, "1-8.2"), filepath.Join(root, "4-1.3")); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDevice(root, "serial:SYNTH-A")
	if err != nil || got != "usb:4-1.3" {
		t.Fatalf("moved device: %q, %v", got, err)
	}
	device(t, root, "5-1", "06b8", "103b", "SYNTH-A")
	if _, err := resolveDevice(root, "serial:SYNTH-A"); err == nil {
		t.Fatal("duplicate serial accepted")
	}
	if _, err := resolveDevice(root, "serial:"); err == nil {
		t.Fatal("empty serial accepted")
	}
}

func TestArgumentPreservation(t *testing.T) {
	root := t.TempDir()
	device(t, root, "1-2", "06b8", "102b", "SYNTH-DT181")
	args := []string{"--instance", "dt295", "--device", "serial:SYNTH-DT181", "--runtime-dir", "/run/pixd", "--group", "--firmware", "/firmware/a file.bin"}
	want := append([]string(nil), args...)
	want[3] = "usb:1-2"
	got, err := daemonArgs(root, args)
	if err != nil || !reflect.DeepEqual(got, want) || args[3] != "serial:SYNTH-DT181" {
		t.Fatalf("got %q, %v; want %q; original %q", got, err, want, args)
	}
	for _, invalid := range [][]string{{"--device"}, {"--device", "usb:1-2", "--device", "usb:1-3"}} {
		if _, err := daemonArgs(root, invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	got, err = daemonArgs(filepath.Join(root, "missing"), []string{"--help"})
	if err != nil || !reflect.DeepEqual(got, []string{"--help"}) {
		t.Fatalf("help passthrough: %q, %v", got, err)
	}
	got, err = daemonArgs(filepath.Join(root, "missing"), []string{"--device", "serial:SYNTH-A"})
	if err != nil || got[1] != "serial:SYNTH-A" {
		t.Fatalf("missing sysfs fallback: %q, %v", got, err)
	}
}

func TestExecProcess(t *testing.T) {
	if os.Getenv("PIXD_LAUNCH_TEST_CHILD") == "1" {
		os.Exit(run(strings.Split(os.Getenv("PIXD_LAUNCH_TEST_ARGS"), "\n")))
	}
	root := t.TempDir()
	pixd := filepath.Join(root, "pixd")
	if err := os.WriteFile(pixd, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\nexit 23\n"), 0755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestExecProcess$")
	command.Env = append(os.Environ(), "PIXD_LAUNCH_TEST_CHILD=1", "PIXD_LAUNCH_TEST_ARGS=--device\nusb:1-2\n--firmware\n/firmware/a file.bin", "PATH="+root)
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 || string(output) != "--device\nusb:1-2\n--firmware\n/firmware/a file.bin\n" {
		t.Fatalf("exec/status/arguments: %q, %v", output, err)
	}
	if err := os.Remove(pixd); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(os.Args[0], "-test.run=^TestExecProcess$")
	command.Env = append(os.Environ(), "PIXD_LAUNCH_TEST_CHILD=1", "PIXD_LAUNCH_TEST_ARGS=--help", "PATH="+root)
	_, err = command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 127 {
		t.Fatalf("missing executable: %v", err)
	}
}
