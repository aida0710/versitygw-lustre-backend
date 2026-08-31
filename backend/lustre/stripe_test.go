// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0.

package lustre

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCreateStagingFileUsesProgressiveLustreLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	var gotName string
	var gotArgs []string

	run := func(name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		return nil, f.Close()
	}

	err := createStagingFile(path, 0644, 4, 1<<30, run)
	if err != nil {
		t.Fatalf("create staging file: %v", err)
	}

	wantArgs := []string{
		"setstripe",
		"--component-end", "1073741824", "--stripe-count", "1",
		"--component-end", "-1", "--stripe-count", "4",
		path,
	}
	if gotName != "lfs" || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command = %q %q, want %q %q", gotName, gotArgs, "lfs", wantArgs)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat staging file: %v", err)
	}
	if got := info.Mode().Perm(); got != fs.FileMode(0644) {
		t.Fatalf("mode = %#o, want 0644", got)
	}
}

func TestCreateStagingFileUsesPortableCreateWithoutStripeOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	called := false
	run := func(string, ...string) ([]byte, error) {
		called = true
		return nil, errors.New("must not run")
	}

	if err := createStagingFile(path, 0644, 0, 0, run); err != nil {
		t.Fatalf("create staging file: %v", err)
	}
	if called {
		t.Fatal("lfs command ran without a stripe option")
	}
}

func TestCreateStagingFileDoesNotSilentlyFallBackAfterLfsFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	run := func(string, ...string) ([]byte, error) {
		return []byte("layout rejected"), errors.New("exit status 1")
	}

	err := createStagingFile(path, 0644, 4, 1<<30, run)
	if err == nil || !strings.Contains(err.Error(), "layout rejected") {
		t.Fatalf("error = %v, want lfs diagnostic", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("staging file should not be created on lfs failure: %v", statErr)
	}
}
