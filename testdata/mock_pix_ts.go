// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if path := os.Getenv("PIX_MOCK_ARGS"); path != "" {
		data, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(path, data, 0600); err != nil {
			panic(err)
		}
	}
	if os.Getenv("PIX_MOCK_MODE") == "fail" {
		fmt.Fprintln(os.Stderr, "synthetic IPC failure")
		os.Exit(7)
	}
	fmt.Fprintln(os.Stderr, "synthetic diagnostic")
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	for i := 0; i < 128; i++ {
		if _, err := os.Stdout.Write(packet); err != nil {
			panic(err)
		}
	}
}
