// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

func runCmd(args ...string) (code int, stdout, stderr string) {
	return runDefs(dashboards(), args...)
}

func runDefs(defs []dashboardDef, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(defs, args, &out, &errb)
	return code, out.String(), errb.String()
}

// minimal is a dashboard with one well-formed panel; span sets that panel's width, and a span
// over 24 makes Build fail.
func minimal(uid string, span uint32) *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder(uid).
		Uid(uid).
		WithPanel(defaultTimeseries().Title("t").Description("d").
			WithTarget(promQuery(`up{job=~"$job"}`, "")).Span(span))
}

// A panel's queries are named as Grafana names them: A to Z, then AA, AB and on.
func TestRefID(t *testing.T) {
	for i, want := range map[int]string{0: "A", 2: "C", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"} {
		if got := refID(i); got != want {
			t.Errorf("refID(%d) = %q, want %q", i, got, want)
		}
	}
}

// The generator numbers only what the builder left unset: the first panel's id 2 and refIds
// "errors" and "total" are kept, its unnamed query gets the first free letter, and the other
// panels get the lowest ids 2 leaves free.
func TestBuildDashboardKeepsSetIDsAndRefIDs(t *testing.T) {
	b := dashboard.NewDashboardBuilder("Set").
		Uid("set").
		WithPanel(defaultTimeseries().Title("preset").Description("d").Id(2).
			WithTarget(promQuery(`rate(e{job=~"$job"}[$__rate_interval])`, "").RefId("errors")).
			WithTarget(promQuery(`up{job=~"$job"}`, "")).
			WithTarget(promQuery(`rate(t{job=~"$job"}[$__rate_interval])`, "").RefId("total"))).
		WithPanel(defaultTimeseries().Title("second").Description("d").
			WithTarget(promQuery(`up{job=~"$job"}`, ""))).
		WithPanel(defaultTimeseries().Title("third").Description("d").
			WithTarget(promQuery(`up{job=~"$job"}`, "")))
	got := map[string]any{}
	for _, p := range panels(built(t, b)) {
		var refs []any
		targets, _ := p["targets"].([]any)
		for _, tg := range targets {
			tm, _ := tg.(map[string]any)
			refs = append(refs, tm["refId"])
		}
		got[p["title"].(string)] = []any{p["id"], refs}
	}
	want := map[string]any{
		"preset": []any{float64(2), []any{"errors", "A", "total"}},
		"second": []any{float64(1), []any{"A"}},
		"third":  []any{float64(3), []any{"A"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ids and refIds %v, want %v", got, want)
	}
}

// realTempDir is t.TempDir with its symbolic links resolved: the generator refuses an output path
// reached through a link, and the temporary directory on macOS is under one, /var.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// manifest returns the lines of dir/.generated.
func manifest(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".generated"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

// The first run, with no .generated yet, removes nothing, writes the dashboards and lists them.
func TestRunFirstRunListsWhatItWrote(t *testing.T) {
	dir := filepath.Join(realTempDir(t), "out")
	if code, _, stderr := runCmd("-output", dir, "-clean"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var want []string
	for _, d := range dashboards() {
		want = append(want, d.name+".json")
	}
	if got := manifest(t, dir); !reflect.DeepEqual(got, want) {
		t.Errorf(".generated lists %v, want %v", got, want)
	}
}

// -clean removes the files the last run listed and are still the generator's: a stale dashboard
// and a stale community copy go. A file the list does not name, a list entry that leaves the
// directory, and one naming a directory are left alone.
func TestRunCleanRemovesOnlyWhatItWrote(t *testing.T) {
	base := realTempDir(t)
	dir := filepath.Join(base, "out")
	files := map[string]string{
		"notes.json":           "mine",
		"stale.json":           "{}",
		"external/old.json":    "{}",
		"../escape.json":       "outside",
		"a-directory.json/x":   "",
		"external/notes2.json": "mine",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := "stale.json\nexternal/old.json\n../escape.json\n" + base + "/out/notes.json\na-directory.json\n"
	if err := os.WriteFile(filepath.Join(dir, ".generated"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCmd("-output", dir, "-clean"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for name, want := range map[string]bool{
		"stale.json": false, "external/old.json": false,
		"notes.json": true, "external/notes2.json": true, "../escape.json": true, "a-directory.json/x": true,
	} {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if got := err == nil; got != want {
			t.Errorf("%s present = %v, want %v", name, got, want)
		}
	}
	for _, rel := range manifest(t, dir) {
		if rel == "stale.json" || rel == "external/old.json" || strings.Contains(rel, "notes") {
			t.Errorf(".generated still lists %s", rel)
		}
	}
}

// Without -clean nothing is removed, and a file an earlier run wrote stays on the list, so a later
// run with -clean still removes it.
func TestRunKeepsEarlierFilesListed(t *testing.T) {
	dir := filepath.Join(realTempDir(t), "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".generated"), []byte("stale.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCmd("-output", dir); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); err != nil {
		t.Errorf("stale.json removed without -clean: %v", err)
	}
	if got := manifest(t, dir); len(got) == 0 || got[0] != "stale.json" {
		t.Errorf(".generated lists %v, want stale.json kept first", got)
	}
	if code, _, stderr := runCmd("-output", dir, "-clean"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); err == nil {
		t.Error("stale.json kept by -clean")
	}
}

// The generator's own source directories are refused, by their resolved path, before anything is
// written: exit 2, naming the directory.
func TestRunRefusesTheSourceDirectories(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(".."); err != nil { // the repository root, where make runs the generator
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()
	for out, src := range map[string]string{
		"dashboards": "dashboards", "./dashboards/": "dashboards", "dashboards/output/..": "dashboards",
		"dashboards/external": "dashboards/external",
	} {
		code, stdout, stderr := runCmd("-output", out, "-clean")
		if code != 2 {
			t.Errorf("-output %s: exit %d, want 2", out, code)
		}
		if want := "-output " + out + " is " + src + "/, which holds the source"; !strings.Contains(stderr, want) {
			t.Errorf("-output %s: stderr %q, want %q", out, stderr, want)
		}
		if stdout != "" {
			t.Errorf("-output %s: wrote %s", out, stdout)
		}
	}
	for _, f := range []string{"dashboards/.generated", "dashboards/asynq-queue-health.json", "dashboards/external/.generated"} {
		if _, err := os.Lstat(f); err == nil {
			t.Errorf("%s written into the source", f)
		}
	}
}

func TestRunWritesEveryDashboard(t *testing.T) {
	dir := filepath.Join(realTempDir(t), "out")
	code, stdout, stderr := runCmd("-output", dir)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	for _, d := range dashboards() {
		path := filepath.Join(dir, d.name+".json")
		if !strings.Contains(stdout, "wrote "+path+"\n") {
			t.Errorf("stdout does not report %s:\n%s", path, stdout)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s is not JSON: %v", path, err)
		}
		if !indented(data) {
			t.Errorf("%s is not indented with two spaces", path)
		}
		if uid, _ := m["uid"].(string); uid == "" {
			t.Errorf("%s has no uid", path)
		}
		ids := map[float64]bool{}
		for _, p := range panelsAndRows(m) {
			id, _ := p["id"].(float64)
			if id <= 0 || ids[id] {
				t.Errorf("%s: %q has id %v, want a positive id no other panel uses", path, p["title"], p["id"])
			}
			ids[id] = true
			targets, _ := p["targets"].([]any)
			for _, tg := range targets {
				if tm, _ := tg.(map[string]any); tm["refId"] == nil || tm["refId"] == "" {
					t.Errorf("%s: panel %q has a query without a refId", path, p["title"])
				}
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(entries), len(dashboards())+2; got != want { // + external/ and .generated
		t.Errorf("output holds %d entries, want %d", got, want)
	}
}

// indented reports whether data is JSON exactly as json.Indent with two spaces writes it.
func indented(data []byte) bool {
	var compact, pretty bytes.Buffer
	if json.Compact(&compact, data) != nil || json.Indent(&pretty, compact.Bytes(), "", "  ") != nil {
		return false
	}
	return bytes.Equal(pretty.Bytes(), data)
}

func TestRunHelp(t *testing.T) {
	code, stdout, stderr := runCmd("-h")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "Usage: go run ./dashboards [-output dir] [-clean]\n" +
		"Writes one Grafana dashboard JSON file per Go builder, then copies dashboards/external/*.json into <dir>/external/, and lists every file it wrote in <dir>/.generated.\n" +
		"  -clean\n" +
		"    \tFirst remove the files the last run wrote, which <dir>/.generated lists\n" +
		"  -output string\n" +
		"    \tDirectory to write the generated dashboard JSON files into (default \".\")\n"
	if stderr != want {
		t.Errorf("help text =\n%s\nwant\n%s", stderr, want)
	}
}

func TestRunUnknownFlag(t *testing.T) {
	code, _, stderr := runCmd("-nope")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined: -nope") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRunOutputIsAFile(t *testing.T) {
	file := filepath.Join(realTempDir(t), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCmd("-output", file)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "failed to create output directory: ") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want the one line naming the directory error", stderr)
	}
}

func TestRunDashboardPathIsADirectory(t *testing.T) {
	dir := realTempDir(t)
	blocked := filepath.Join(dir, dashboards()[0].name+".json")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCmd("-output", dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "failed to write "+blocked+": ") {
		t.Errorf("stderr = %q", stderr)
	}
}

// A directory given without -output is a mistake, not a request to write into ".": it exits 2,
// says why, and writes nothing.
func TestRunRejectsPositionalArguments(t *testing.T) {
	dir := realTempDir(t)
	code, stdout, stderr := runCmd("-output", dir, "out")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, `unexpected argument "out": name the output directory with -output`+"\n"+"Usage: go run ./dashboards [-output dir] [-clean]\n") {
		t.Errorf("stderr = %q", stderr)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("wrote %d entries, want none", len(entries))
	}
}

// A dashboard that fails to build stops the run: exit 1, and nothing after it is written.
func TestRunStopsAtABuildError(t *testing.T) {
	dir := realTempDir(t)
	defs := []dashboardDef{
		{name: "first", builder: minimal("first", 24)},
		{name: "broken", builder: minimal("broken", 25)},
		{name: "after", builder: minimal("after", 24)},
	}
	code, _, stderr := runDefs(defs, "-output", dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, `failed to build dashboard "broken": `) {
		t.Errorf("stderr = %q", stderr)
	}
	for name, want := range map[string]bool{"first.json": true, "broken.json": false, "after.json": false, "external": false} {
		_, err := os.Stat(filepath.Join(dir, name))
		if got := err == nil; got != want {
			t.Errorf("%s written = %v, want %v", name, got, want)
		}
	}
}

// Two builders with one uid: Grafana would keep only one, so the run exits 1 naming both files
// and writes nothing after the second.
func TestRunRejectsDuplicateUIDs(t *testing.T) {
	dir := realTempDir(t)
	defs := []dashboardDef{
		{name: "a", builder: minimal("same", 24)},
		{name: "b", builder: minimal("same", 24)},
		{name: "c", builder: minimal("other", 24)},
	}
	code, _, stderr := runDefs(defs, "-output", dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stderr != `uid "same" is used by both a.json and b.json`+"\n" {
		t.Errorf("stderr = %q", stderr)
	}
	// Nothing after the conflict is written: not b.json, not the later c.json, not external/.
	for _, f := range []string{"b.json", "c.json", "external"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s written", f)
		}
	}
}
