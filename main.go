// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const usage = "usage: pix-mirakc-tuner --type GR|BS|CS --channel CH --duration SECONDS|- --instance TOKEN [--runtime-dir DIR] [--group] [--slot 0..7|--tsid 1..65534]"

type options struct {
	kind, channel, duration, instance, runtimeDir string
	selection, selectionValue                     string
	group, hasRuntime                             bool
}

func invalid(message string) error { return fmt.Errorf("pix-mirakc-tuner: %s", message) }

func parse(args []string) ([]string, error) {
	var opt options
	seen := make(map[string]bool)
	for len(args) > 0 {
		name := args[0]
		if name == "--group" {
			if seen[name] {
				return nil, invalid("duplicate --group")
			}
			seen[name], opt.group = true, true
			args = args[1:]
			continue
		}
		switch name {
		case "--type", "--channel", "--duration", "--instance", "--runtime-dir", "--slot", "--tsid":
		default:
			return nil, fmt.Errorf("%s", usage)
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("%s", usage)
		}
		value := args[1]
		if value == "" {
			return nil, invalid("empty " + name)
		}
		if name == "--slot" || name == "--tsid" {
			if opt.selection != "" {
				return nil, invalid("satellite selection specified twice")
			}
			opt.selection, opt.selectionValue = name, value
		} else {
			if seen[name] {
				return nil, invalid("duplicate " + name)
			}
			seen[name] = true
			switch name {
			case "--type":
				opt.kind = value
			case "--channel":
				opt.channel = value
			case "--duration":
				opt.duration = value
			case "--instance":
				opt.instance = value
			case "--runtime-dir":
				opt.runtimeDir, opt.hasRuntime = value, true
			}
		}
		args = args[2:]
	}
	for _, name := range []string{"--type", "--channel", "--duration", "--instance"} {
		if !seen[name] {
			return nil, fmt.Errorf("%s", usage)
		}
	}
	if opt.kind != "GR" && opt.kind != "BS" && opt.kind != "CS" {
		return nil, invalid("type must be GR, BS or CS")
	}
	if !validInstance(opt.instance) {
		return nil, invalid("invalid instance")
	}
	if opt.hasRuntime && !strings.HasPrefix(opt.runtimeDir, "/") {
		return nil, invalid("runtime-dir must be absolute")
	}
	if opt.group && !opt.hasRuntime {
		return nil, invalid("--group requires --runtime-dir")
	}
	if opt.duration != "-" && !positiveDecimal(opt.duration) {
		return nil, invalid("duration must be positive decimal seconds or -")
	}

	// @TSID is the adapter's notation for a mirakc channel with an explicit TSID.
	if strings.Contains(opt.channel, "@") {
		if opt.selection != "" {
			return nil, invalid("satellite selection specified twice")
		}
		opt.channel, opt.selectionValue, _ = strings.Cut(opt.channel, "@")
		opt.selection = "--tsid"
	}
	var system string
	switch opt.kind {
	case "GR":
		if opt.selection != "" {
			return nil, invalid("GR cannot select a satellite stream")
		}
		if len(opt.channel) == 2 && decimal(opt.channel) {
			opt.channel = "T" + opt.channel
		}
		if !validGR(opt.channel) {
			return nil, invalid("GR channel must be T13..T62")
		}
		system = "isdb-t"
	case "BS", "CS":
		if strings.Contains(opt.channel, "_") {
			if opt.selection != "" {
				return nil, invalid("satellite selection specified twice")
			}
			opt.channel, opt.selectionValue, _ = strings.Cut(opt.channel, "_")
			opt.selection = "--slot"
		}
		if opt.kind == "BS" {
			if !validSatellite(opt.channel, "BS", 1) {
				return nil, invalid("BS channel must be an odd BS01..BS23 transponder")
			}
			if opt.selection == "" {
				return nil, invalid("BS requires a slot or TSID")
			}
		} else {
			if !validSatellite(opt.channel, "CS", 0) {
				return nil, invalid("CS channel must be an even CS02..CS24 transponder")
			}
			if opt.selection == "" {
				return nil, invalid("CS requires an explicit slot or TSID")
			}
		}
		system = "isdb-s"
	}
	if opt.selection == "--slot" && (len(opt.selectionValue) != 1 || opt.selectionValue[0] < '0' || opt.selectionValue[0] > '7') {
		return nil, invalid("DT295 satellite slot must be 0..7")
	}
	if opt.selection == "--tsid" {
		if !positiveDecimal(opt.selectionValue) || len(opt.selectionValue) > 5 {
			return nil, invalid("TSID must be decimal 1..65534")
		}
		value, err := strconv.Atoi(opt.selectionValue)
		if err != nil || value > 65534 {
			return nil, invalid("TSID must be decimal 1..65534")
		}
	}
	result := []string{"--instance", opt.instance, "--system", system, "--channel", opt.channel, "--time", opt.duration, "--output", "-"}
	if opt.hasRuntime {
		result = append(result, "--runtime-dir", opt.runtimeDir)
	}
	if opt.group {
		result = append(result, "--group")
	}
	if opt.selection != "" {
		result = append(result, opt.selection, opt.selectionValue)
	}
	return result, nil
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func positiveDecimal(value string) bool {
	return len(value) > 0 && value[0] >= '1' && value[0] <= '9' && decimal(value)
}

func validInstance(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[len(value)-1] == '.' {
		return false
	}
	for index, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func validGR(value string) bool {
	if len(value) != 3 || value[0] != 'T' || !decimal(value[1:]) {
		return false
	}
	channel, _ := strconv.Atoi(value[1:])
	return channel >= 13 && channel <= 62
}

func validSatellite(value, prefix string, remainder int) bool {
	if len(value) != 4 || !strings.HasPrefix(value, prefix) || !decimal(value[2:]) {
		return false
	}
	channel, _ := strconv.Atoi(value[2:])
	if prefix == "BS" {
		return channel >= 1 && channel <= 23 && channel%2 == remainder
	}
	return channel >= 2 && channel <= 24 && channel%2 == remainder
}

func run(args []string) int {
	pixArgs, err := parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pixTS := os.Getenv("PIX_TS_BIN")
	if pixTS == "" {
		pixTS = "pix-ts"
	}
	path, err := exec.LookPath(pixTS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pix-mirakc-tuner: pix-ts executable: %v\n", err)
		return 127
	}
	env := append(os.Environ(), "LC_ALL=C")
	if err := syscall.Exec(path, append([]string{pixTS}, pixArgs...), env); err != nil {
		fmt.Fprintf(os.Stderr, "pix-mirakc-tuner: exec pix-ts: %v\n", err)
		return 126
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:])) }
