// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0.

package lustre

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type commandRunner func(name string, args ...string) ([]byte, error)

func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// createStagingFile creates the sparse file that receives multipart parts.
// When stripeCount is greater than one, a progressive Lustre layout keeps the
// first component on one OST and stripes only the large-file tail. This avoids
// multiplying OST fan-out for small objects while allowing large objects to
// use several OSTs in parallel.
func createStagingFile(path string, perm fs.FileMode, stripeCount int,
	stripeThreshold int64, run commandRunner) error {
	if stripeCount <= 1 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err != nil {
			return err
		}
		return f.Close()
	}

	if stripeThreshold <= 0 {
		return fmt.Errorf("a positive progressive stripe threshold is required with stripe count %d", stripeCount)
	}
	if run == nil {
		run = runCommand
	}

	args := []string{
		"setstripe",
		"--component-end", strconv.FormatInt(stripeThreshold, 10),
		"--stripe-count", "1",
		"--component-end", "-1",
		"--stripe-count", strconv.Itoa(stripeCount),
		path,
	}
	out, err := run("lfs", args...)
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("create progressive Lustre staging file: %w", err)
		}
		return fmt.Errorf("create progressive Lustre staging file: %w: %s", err, detail)
	}

	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("set staging file permissions: %w", err)
	}
	return nil
}
