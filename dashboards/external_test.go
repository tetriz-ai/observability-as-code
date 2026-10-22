// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const noCommunityNotice = "no community dashboards in dashboards/external/ (make dashboards-sync fetches them)\n"

func TestEmitExternalDashboards(t *testing.T) {
	scaffold := fstest.MapFS{
		"external/manifest.yaml":    {Data: []byte("dashboards: []\n")},
		"external/README.md":        {Data: []byte("# External\n")},
		"external/scripts/sync.sh":  {Data: []byte("#!/bin/sh\n")},
		"external/nested/skip.json": {Data: []byte("not json, and not at the top level\n")},
	}
	with := func(files map[string]string) fstest.MapFS {
		m := fstest.MapFS{}
		for k, v := range scaffold {
			m[k] = v
		}
		for k, v := range files {
			m["external/"+k] = &fstest.MapFile{Data: []byte(v)}
		}
		return m
	}

	tests := []struct {
		name      string
		fsys      fstest.MapFS
		wantErr   string
		wantFiles []string
		wantOut   string
	}{
		{
			name:      "copies each JSON and skips everything else",
			fsys:      with(map[string]string{"a.json": `{"uid":"a","title":"A","panels":[]}`, "b.json": `{"uid":"b","title":"B"}`}),
			wantFiles: []string{"a.json", "b.json"},
		},
		{
			name:      "a name starting with _ is copied like any other",
			fsys:      with(map[string]string{"_team.json": `{"uid":"team","title":"Team"}`}),
			wantFiles: []string{"_team.json"},
		},
		{
			name:      "a name starting with . is copied like any other",
			fsys:      with(map[string]string{".hidden.json": `{"uid":"hidden","title":"Hidden"}`}),
			wantFiles: []string{".hidden.json"},
		},
		{
			name:      "a macOS AppleDouble file beside a dashboard is skipped",
			fsys:      with(map[string]string{"k8s.json": `{"uid":"k8s","title":"K8s"}`, "._k8s.json": "\x00\x05\x16\x07junk"}),
			wantFiles: []string{"k8s.json"},
		},
		{
			name:    "two community dashboards sharing a uid",
			fsys:    with(map[string]string{"a.json": `{"uid":"same","title":"A"}`, "b.json": `{"uid":"same","title":"B"}`}),
			wantErr: `uid "same" is used by both external/a.json and external/b.json`,
		},
		{
			name:      "one JSON prints no notice",
			fsys:      with(map[string]string{"one.json": `{"uid":"one","title":"One"}`}),
			wantFiles: []string{"one.json"},
		},
		{
			name:    "no JSON says so",
			fsys:    with(nil),
			wantOut: noCommunityNotice,
		},
		{
			name:    "invalid JSON",
			fsys:    with(map[string]string{"bad.json": `{"uid":`}),
			wantErr: "parsing external/bad.json",
		},
		{
			name:    "missing uid",
			fsys:    with(map[string]string{"x.json": `{"title":"X"}`}),
			wantErr: "external/x.json: missing or non-string 'uid'",
		},
		{
			name:    "numeric uid",
			fsys:    with(map[string]string{"x.json": `{"uid":7,"title":"X"}`}),
			wantErr: "external/x.json: missing or non-string 'uid'",
		},
		{
			name:    "null uid",
			fsys:    with(map[string]string{"x.json": `{"uid":null,"title":"X"}`}),
			wantErr: "external/x.json: missing or non-string 'uid'",
		},
		{
			name:    "missing title",
			fsys:    with(map[string]string{"x.json": `{"uid":"x"}`}),
			wantErr: "external/x.json: missing or non-string 'title'",
		},
		{
			name:    "uid a generated dashboard already uses",
			fsys:    with(map[string]string{"x.json": `{"uid":"asynq-queue-health","title":"X"}`}),
			wantErr: `uid "asynq-queue-health" is used by both asynq-queue-health.json and external/x.json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "external")
			var stdout bytes.Buffer
			uids := map[string]string{"asynq-queue-health": "asynq-queue-health.json"}
			_, err := emitExternalDashboards(tt.fsys, out, uids, &stdout)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range entries {
				got = append(got, e.Name())
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFiles, ",") {
				t.Errorf("files = %v, want %v", got, tt.wantFiles)
			}
			if tt.wantOut != "" && stdout.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantOut)
			}
			for _, f := range tt.wantFiles {
				if !strings.Contains(stdout.String(), "wrote "+filepath.Join(out, f)+"\n") {
					t.Errorf("stdout does not report %s: %q", f, stdout.String())
				}
				if strings.Contains(stdout.String(), noCommunityNotice) {
					t.Errorf("notice printed although %s was copied", f)
				}
			}
		})
	}
}

// A community dashboard whose uid a generated dashboard already uses is not written, and nor is
// any file after it: the run stops there. Files are read in name order, so a.json comes before.
func TestEmitExternalDashboardsWritesNoConflictingFile(t *testing.T) {
	fsys := fstest.MapFS{
		"external/a.json": {Data: []byte(`{"uid":"a","title":"A"}`)},
		"external/x.json": {Data: []byte(`{"uid":"asynq-queue-health","title":"X"}`)},
		"external/z.json": {Data: []byte(`{"uid":"z","title":"Z"}`)},
	}
	out := filepath.Join(t.TempDir(), "external")
	uids := map[string]string{"asynq-queue-health": "asynq-queue-health.json"}
	if _, err := emitExternalDashboards(fsys, out, uids, &bytes.Buffer{}); err == nil {
		t.Fatal("no error for a uid already in use")
	}
	if _, err := os.Stat(filepath.Join(out, "a.json")); err != nil {
		t.Errorf("a.json, before the conflict, was not written: %v", err)
	}
	for _, f := range []string{"x.json", "z.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err == nil {
			t.Errorf("%s was written", f)
		}
	}
}

// A community dashboard is re-indented with two spaces, whatever its spacing as fetched.
func TestEmitExternalDashboardsIndents(t *testing.T) {
	src := `{"uid":"c","title":"C","panels":[{"id":1}]}`
	fsys := fstest.MapFS{"external/c.json": {Data: []byte(src)}}
	out := filepath.Join(t.TempDir(), "external")
	if _, err := emitExternalDashboards(fsys, out, map[string]string{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"uid\": \"c\",\n  \"title\": \"C\",\n  \"panels\": [\n    {\n      \"id\": 1\n    }\n  ]\n}"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A community dashboard is written as fetched, only re-indented: an integer past float64's
// exact range and bytes that are not valid UTF-8 come out as they went in.
func TestEmitExternalDashboardsKeepsBytes(t *testing.T) {
	src := "{\"uid\":\"big\",\"title\":\"Big \xff\",\"panels\":[{\"id\":9007199254740993}]}"
	fsys := fstest.MapFS{"external/big.json": {Data: []byte(src)}}
	out := filepath.Join(t.TempDir(), "external")
	if _, err := emitExternalDashboards(fsys, out, map[string]string{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "big.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"9007199254740993", "\"Big \xff\""} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

// The embedded directory is the whole of external/, so the build holds with no fetched JSON.
func TestEmbeddedExternalDirectory(t *testing.T) {
	if _, err := fs.Stat(externalDashboards, "external/manifest.yaml"); err != nil {
		t.Errorf("manifest not embedded: %v", err)
	}
}
