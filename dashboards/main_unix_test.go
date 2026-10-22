// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Grafana reads the output as its own user when the directory is mounted into a container, so
// every file must be readable and every directory traversable by group and others.
// The process umask is set to the common 022 for the test, so a stricter umask on the machine
// running it does not mask what the code requests.
func TestRunOutputIsReadableByOtherUsers(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	dir := filepath.Join(realTempDir(t), "out")
	if code, _, stderr := runCmd("-output", dir); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		need := os.FileMode(0o044)
		if d.IsDir() {
			need = 0o055
		}
		if perm&need != need {
			t.Errorf("%s has mode %v: group and others cannot read it", path, perm)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// sentinel is a directory outside every output path, holding JSON and a .generated that lists it,
// so a generator that followed a link and cleaned there would delete it.
func sentinel(t *testing.T) string {
	t.Helper()
	dir := realTempDir(t)
	for _, f := range []string{"keep.json", "external/keep2.json", "sub/keep3.json"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for dir, list := range map[string]string{dir: "keep.json\nexternal/keep2.json\n", filepath.Join(dir, "sub"): "keep3.json\n"} {
		if err := os.WriteFile(filepath.Join(dir, ".generated"), []byte(list), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// untouched fails the test unless the sentinel directory holds exactly what sentinel left.
func untouched(t *testing.T, dir string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		b, _ := os.ReadFile(path)
		got = append(got, rel+"="+strings.ReplaceAll(string(b), "\n", ","))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".generated=keep.json,external/keep2.json,", "external/keep2.json=keep", "keep.json=keep", "sub/.generated=keep3.json,", "sub/keep3.json=keep"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the directory outside holds %v, want %v", got, want)
	}
}

// An output path with a symbolic link in it, at its end, before a trailing "/." or "/", above it,
// or at its external/, would send the cleanup and the files outside the directory named. The
// generator exits 2 naming the path, before it removes or writes anything.
func TestRunRefusesALinkInTheOutputPath(t *testing.T) {
	for _, c := range []struct{ name, link, target, output string }{
		{"link", "out-link", "", "out-link"},
		{"link/.", "out-link", "", "out-link/."},
		{"link/", "out-link", "", "out-link/"},
		{"ancestor", "out-parent", "", "out-parent/sub"},
		{"external", "out/external", "external", "out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, outside := realTempDir(t), sentinel(t)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(base, c.link)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, c.target), filepath.Join(base, c.link)); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runCmd("-output", base+"/"+c.output, "-clean")
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if !strings.Contains(stderr, "is a symbolic link or is reached through one") {
				t.Errorf("stderr %q does not name the link", stderr)
			}
			if stdout != "" {
				t.Errorf("wrote files: %s", stdout)
			}
			untouched(t, outside)
		})
	}
}

// A file the generator would write that is a symbolic link is refused before anything is removed
// or written: exit 2, and the file it points at keeps its content, even when .generated lists it.
func TestRunRefusesToWriteThroughASymlink(t *testing.T) {
	out := filepath.Join(realTempDir(t), "out")
	outside := sentinel(t)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(out, dashboards()[0].name+".json")
	if err := os.Symlink(filepath.Join(outside, "keep.json"), link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, ".generated"), []byte(dashboards()[0].name+".json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCmd("-output", out, "-clean")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, link+" is a symbolic link") {
		t.Errorf("stderr %q does not name %s", stderr, link)
	}
	if stdout != "" {
		t.Errorf("wrote files: %s", stdout)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the link was removed: %v", err)
	}
	untouched(t, outside)
}
