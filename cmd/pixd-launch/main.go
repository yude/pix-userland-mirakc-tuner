// SPDX-License-Identifier: MIT
// Resolve a Linux cached USB serial before executing the unmodified pixd.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

var usbPort = regexp.MustCompile(`^[0-9]+-[0-9]+(?:\.[0-9]+)*$`)

func attribute(root, port, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, port, name))
	return strings.TrimSuffix(string(data), "\n"), err
}

// A missing cache leaves the existing serial selector for pixd to handle.
// Multiple matching devices must never silently choose the first one.
func resolveDevice(root, selector string) (string, error) {
	if !strings.HasPrefix(selector, "serial:") {
		return selector, nil
	}
	serial := strings.TrimPrefix(selector, "serial:")
	if serial == "" || len(serial) > 512 || !utf8.ValidString(serial) || strings.ContainsFunc(serial, unicode.IsControl) {
		return "", fmt.Errorf("invalid serial selector")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return selector, nil
	}
	if err != nil {
		return "", fmt.Errorf("read USB sysfs: %w", err)
	}
	var matches []string
	for _, entry := range entries {
		port := entry.Name()
		if !usbPort.MatchString(port) {
			continue
		}
		vendor, e1 := attribute(root, port, "idVendor")
		product, e2 := attribute(root, port, "idProduct")
		cached, e3 := attribute(root, port, "serial")
		if e1 != nil || e2 != nil || e3 != nil || vendor != "06b8" ||
			(product != "103b" && product != "102b") || cached != serial {
			continue
		}
		matches = append(matches, port)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("serial matches multiple PIX devices; use an explicit usb: selector")
	}
	if len(matches) == 1 {
		return "usb:" + matches[0], nil
	}
	return selector, nil
}

func daemonArgs(root string, args []string) ([]string, error) {
	result := append([]string(nil), args...)
	device := -1
	for i := 0; i < len(args); i++ {
		if args[i] != "--device" {
			continue
		}
		if device >= 0 || i+1 == len(args) {
			return nil, fmt.Errorf("--device requires one selector and may be used once")
		}
		device = i + 1
		i++
	}
	if device >= 0 {
		resolved, err := resolveDevice(root, args[device])
		if err != nil {
			return nil, err
		}
		result[device] = resolved
	}
	return result, nil
}

func run(args []string) int {
	args, err := daemonArgs("/sys/bus/usb/devices", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pixd-launch:", err)
		return 2
	}
	path, err := exec.LookPath("pixd")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pixd-launch:", err)
		return 127
	}
	if err := syscall.Exec(path, append([]string{path}, args...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "pixd-launch:", err)
		return 126
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:])) }
