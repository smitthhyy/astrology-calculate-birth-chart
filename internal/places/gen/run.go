package main

import (
	"bytes"
	"fmt"
	"os/exec"
)

// runCommand runs a command and returns its standard output.
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, errBuf.String())
	}
	return out.String(), nil
}
