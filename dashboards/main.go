// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command dashboards writes one Grafana dashboard JSON file per entry in dashboards(), then
// copies each JSON file in external/ into <dir>/external/, and lists every file it wrote in
// <dir>/.generated. With -clean it first removes the files the last run listed there, and no
// other file.
//
// Usage:
//
//	go run ./dashboards [-output dir] [-clean]
//
// It exits 0 on success or -h, 1 when a dashboard fails to build or write or two dashboards share
// a uid, and 2 on an unknown flag, a positional argument, an output path with a symbolic link in
// it, the generator's source directory, or a file to write that is a symbolic link.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
)

type dashboardDef struct {
	name    string
	builder *dashboard.DashboardBuilder
}

// The whole directory is embedded, not external/*.json: a pattern that matches no file fails
// the build, and the fetched JSONs are optional. all: keeps names that start with "_" or ".",
// which a plain directory embed leaves out. emitExternalDashboards skips non-JSON entries.
//
//go:embed all:external
var externalDashboards embed.FS

// Output files are world-readable: they are not secrets, and Grafana reads them as its own
// user when the output directory is mounted into a container.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// dashboards lists every generated dashboard; name is the output file name without ".json".
func dashboards() []dashboardDef {
	return []dashboardDef{
		{name: "asynq-queue-health", builder: asynqQueueDashboard()},
		{name: "redis-command-health", builder: redisCommandDashboard()},
	}
}

func main() {
	if code := run(dashboards(), os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// run is the command without the process exit: 0 on success or -h, 1 when a dashboard cannot
// be built or written or two dashboards share a uid, and 2 on a flag error, a stray argument, or
// an output path the checks in output.go refuse, before anything is removed or written. It stops
// at the first failure, so nothing after it is written, and lists what it did write.
func run(defs []dashboardDef, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dashboards", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputDir := flags.String("output", ".", "Directory to write the generated dashboard JSON files into")
	clean := flags.Bool("clean", false, "First remove the files the last run wrote, which <dir>/.generated lists")
	flags.Usage = func() {
		say(stderr, "Usage: go run ./dashboards [-output dir] [-clean]\n")
		say(stderr, "Writes one Grafana dashboard JSON file per Go builder, then copies dashboards/external/*.json into <dir>/external/, and lists every file it wrote in <dir>/.generated.\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		say(stderr, "unexpected argument %q: name the output directory with -output\n", flags.Arg(0))
		flags.Usage()
		return 2
	}

	// Everything that could send a removal or a write outside the output directory is refused
	// before anything is touched.
	abs, err := resolveOutput(*outputDir)
	if err != nil {
		say(stderr, "%v\n", err)
		return 2
	}
	external, err := externalFiles(externalDashboards)
	if err != nil {
		say(stderr, "%v\n", err)
		return 1
	}
	planned := make([]string, 0, len(defs)+len(external))
	for _, d := range defs {
		planned = append(planned, d.name+".json")
	}
	for _, name := range external {
		planned = append(planned, "external/"+name)
	}
	if err := refuseLinks(abs, planned); err != nil {
		say(stderr, "%v\n", err)
		return 2
	}

	if err := os.MkdirAll(abs, dirMode); err != nil {
		say(stderr, "failed to create output directory: %v\n", err)
		return 1
	}
	if *clean {
		if err := removeGenerated(abs); err != nil {
			say(stderr, "failed to remove the files the last run wrote: %v\n", err)
			return 1
		}
	}
	var written []string // relative to the output directory, as .generated lists them
	finish := func(code int) int {
		if err := writeManifest(abs, written); err != nil {
			say(stderr, "failed to write %s: %v\n", filepath.Join(*outputDir, manifestName), err)
			if code == 0 {
				code = 1
			}
		}
		return code
	}

	uids := map[string]string{} // uid -> the file that carries it
	for _, d := range defs {
		dash, err := buildDashboard(d.builder)
		if err != nil {
			say(stderr, "failed to build dashboard %q: %v\n", d.name, err)
			return finish(1)
		}

		file := d.name + ".json"
		if dash.Uid != nil {
			if err := claimUID(uids, *dash.Uid, file); err != nil {
				say(stderr, "%v\n", err)
				return finish(1)
			}
		}

		data, err := json.MarshalIndent(dash, "", "  ")
		if err != nil {
			say(stderr, "failed to marshal dashboard %q: %v\n", d.name, err)
			return finish(1)
		}

		path := filepath.Join(*outputDir, file)
		if err := writeFile(filepath.Join(abs, file), data); err != nil {
			say(stderr, "failed to write %s: %v\n", path, err)
			return finish(1)
		}
		written = append(written, file)
		say(stdout, "wrote %s\n", path)
	}

	copied, err := emitExternalDashboards(externalDashboards, filepath.Join(*outputDir, "external"), uids, stdout)
	for _, name := range copied {
		written = append(written, "external/"+name)
	}
	if err != nil {
		say(stderr, "failed to emit external dashboards: %v\n", err)
		return finish(1)
	}
	return finish(0)
}

// buildDashboard builds b and numbers what the builder left unnumbered, keeping every id and
// refId it set. A panel or row without an id gets the lowest number no other panel or row uses,
// in layout order. A Prometheus query without a refId gets the first name (A, B, C, ...) no
// other query of its panel uses. Grafana's query API keys each result by refId, so a script
// that sends a panel's queries there needs them, and a link to one panel needs its id.
func buildDashboard(b *dashboard.DashboardBuilder) (dashboard.Dashboard, error) {
	d, err := b.Build()
	if err != nil {
		return d, err
	}
	used := map[uint32]bool{}
	var unset []*uint32
	claim := func(id *uint32) {
		if *id == 0 {
			unset = append(unset, id)
		} else {
			used[*id] = true
		}
	}
	panel := func(p *dashboard.Panel) {
		if p.Id == nil {
			p.Id = new(uint32)
		}
		claim(p.Id)
		nameQueries(p)
	}
	for i := range d.Panels {
		if row := d.Panels[i].RowPanel; row != nil {
			claim(&row.Id)
			for j := range row.Panels {
				panel(&row.Panels[j])
			}
		}
		if p := d.Panels[i].Panel; p != nil {
			panel(p)
		}
	}
	next := uint32(1)
	for _, id := range unset {
		for used[next] {
			next++
		}
		*id = next
		next++
	}
	return d, nil
}

// nameQueries gives each Prometheus query of p without a refId the first name, by refID, that
// no other query of p uses.
func nameQueries(p *dashboard.Panel) {
	used := map[string]bool{}
	for _, t := range p.Targets {
		if q, ok := t.(prometheus.Dataquery); ok && q.RefId != nil && *q.RefId != "" {
			used[*q.RefId] = true
		}
	}
	n := 0
	for i, t := range p.Targets {
		q, ok := t.(prometheus.Dataquery)
		if !ok || (q.RefId != nil && *q.RefId != "") {
			continue
		}
		ref := refID(n)
		for used[ref] {
			n++
			ref = refID(n)
		}
		n++
		used[ref] = true
		q.RefId = &ref
		p.Targets[i] = q
	}
}

// refID is Grafana's name for a panel's i-th query, counting from 0: A to Z, then AA, AB and on.
func refID(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// claimUID records that file carries uid, or fails when another file already does: Grafana
// keeps one dashboard per uid, so the second import would replace the first.
func claimUID(uids map[string]string, uid, file string) error {
	if prev, ok := uids[uid]; ok {
		return fmt.Errorf("uid %q is used by both %s and %s", uid, prev, file)
	}
	uids[uid] = file
	return nil
}

// emitExternalDashboards copies every JSON file under external/ in fsys into outDir. sync.sh
// normalizes each file when it fetches it (id removed, uid pinned, datasource inputs declared
// as variables). Here each file is checked for a string uid and title and a uid no other
// dashboard uses, then written as fetched, re-indented: its bytes are not re-encoded, so large
// numbers and every string survive exactly. When external/ holds no JSON it says so on stdout
// rather than emitting nothing in silence.
// It returns the names of the files it wrote, in outDir.
func emitExternalDashboards(fsys fs.FS, outDir string, uids map[string]string, stdout io.Writer) ([]string, error) {
	names, err := externalFiles(fsys)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(outDir, dirMode); err != nil {
		return nil, fmt.Errorf("creating %s: %w", outDir, err)
	}

	var copied []string
	for _, name := range names {
		src := "external/" + name
		data, err := fs.ReadFile(fsys, src)
		if err != nil {
			return copied, fmt.Errorf("reading %s: %w", src, err)
		}

		var head struct {
			UID   json.RawMessage `json:"uid"`
			Title json.RawMessage `json:"title"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			return copied, fmt.Errorf("parsing %s: %w", src, err)
		}
		uid, ok := jsonString(head.UID)
		if !ok {
			return copied, fmt.Errorf("%s: missing or non-string 'uid' — normalize before committing", src)
		}
		if _, ok := jsonString(head.Title); !ok {
			return copied, fmt.Errorf("%s: missing or non-string 'title'", src)
		}
		if err := claimUID(uids, uid, src); err != nil {
			return copied, err
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			return copied, fmt.Errorf("parsing %s: %w", src, err)
		}
		out := filepath.Join(outDir, name)
		if err := writeFile(out, pretty.Bytes()); err != nil {
			return copied, fmt.Errorf("writing %s: %w", out, err)
		}
		say(stdout, "wrote %s\n", out)
		copied = append(copied, name)
	}
	if len(copied) == 0 {
		say(stdout, "no community dashboards in dashboards/external/ (make dashboards-sync fetches them)\n")
	}
	return copied, nil
}

// externalFiles returns the names of the JSON files under external/ in fsys that the generator
// copies: every one but a macOS AppleDouble metadata file, named ._<name>, which some volumes write
// beside a file.
func externalFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, "external")
	if err != nil {
		return nil, fmt.Errorf("reading embedded external/: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), "._") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// jsonString decodes raw when it is a JSON string. A missing field, null, a number or an object
// is not.
func jsonString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// say writes one line of progress or diagnostics. A write to the terminal that fails cannot be
// reported anywhere better, so its error is dropped on purpose.
func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}
