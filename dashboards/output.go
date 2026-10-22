// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The generator touches only what it owns. Before anything else it checks that the output
// directory and its external/ reach no symbolic link and are not its own source. Each run lists
// the files it wrote in <dir>/.generated, and a later run with -clean removes the files that
// list names, which must be regular files inside the directory, and nothing else.

// manifestName is the file in the output directory that lists, one relative path per line, the
// files the generator wrote there.
const manifestName = ".generated"

// resolveOutput returns the absolute directory -output dir names, after checking it. A relative
// dir is taken from the current directory with that directory's own links resolved, so a clone
// reached through a symbolic link still works; every part of the path dir adds, and every part
// of an absolute dir, must be a real directory, and so must its external/. It also refuses the
// generator's source directories, dashboards/ and dashboards/external/.
func resolveOutput(dir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if cwd, err = filepath.EvalSymlinks(cwd); err != nil {
		return "", err
	}
	abs := filepath.Clean(dir)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, dir)
	}
	for _, p := range []string{abs, filepath.Join(abs, "external")} {
		existing := nearestExisting(p)
		resolved, err := filepath.EvalSymlinks(existing)
		if err != nil {
			return "", fmt.Errorf("-output %s: %v", dir, err)
		}
		if resolved != existing {
			return "", fmt.Errorf("-output %s: %s is a symbolic link or is reached through one (it resolves to %s), so the files would land outside the directory named: give a path with no symbolic link in it", dir, existing, resolved)
		}
	}
	for _, src := range []string{"dashboards", "dashboards/external"} {
		if resolved, err := filepath.EvalSymlinks(filepath.Join(cwd, src)); err == nil && resolved == abs {
			return "", fmt.Errorf("-output %s is %s/, which holds the source: choose another directory", dir, src)
		}
	}
	return abs, nil
}

// nearestExisting returns p, or the closest of its parents that exists.
func nearestExisting(p string) string {
	for {
		if _, err := os.Lstat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// refuseLinks fails when one of the files a run would write in abs, or its manifest, is a
// symbolic link: writing it would replace the link's target, outside the directory.
func refuseLinks(abs string, files []string) error {
	for _, f := range append(files, manifestName) {
		p := filepath.Join(abs, filepath.FromSlash(f))
		if info, err := os.Lstat(p); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link: remove it, so the generator does not write through it", p)
		}
	}
	return nil
}

// readManifest returns the relative paths abs/.generated lists, or none when there is no manifest.
func readManifest(abs string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(abs, manifestName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			files = append(files, line)
		}
	}
	return files, sc.Err()
}

// owned reports whether rel, a path from the manifest, names a regular file inside abs: a local
// path, not a symbolic link, in a directory reached through no link.
func owned(abs, rel string) bool {
	p := filepath.FromSlash(rel)
	if !filepath.IsLocal(p) {
		return false
	}
	full := filepath.Join(abs, p)
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	dir := filepath.Dir(full)
	resolved, err := filepath.EvalSymlinks(dir)
	return err == nil && resolved == dir
}

// removeGenerated removes the files the previous run listed in abs/.generated that are still
// owned, and leaves every other file in the directory alone.
func removeGenerated(abs string) error {
	files, err := readManifest(abs)
	if err != nil {
		return err
	}
	for _, rel := range files {
		if owned(abs, rel) {
			if err := os.Remove(filepath.Join(abs, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeManifest records in abs/.generated the files this run wrote, together with those an
// earlier run listed that are still there, so a file the generator wrote stays its own until a
// run with -clean removes it.
func writeManifest(abs string, written []string) error {
	previous, err := readManifest(abs)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var b strings.Builder
	for _, rel := range append(previous, written...) {
		if seen[rel] || !owned(abs, rel) {
			continue
		}
		seen[rel] = true
		b.WriteString(rel + "\n")
	}
	return writeFile(filepath.Join(abs, manifestName), []byte(b.String()))
}

// writeFile writes data to path through a temporary file in the same directory and a rename, so
// a reader never sees half a file. It refuses a path that is a symbolic link, which a plain write
// would follow out of the directory, or a directory.
func writeFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link", path)
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", path)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails harmlessly once the rename has moved it
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
