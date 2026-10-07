package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// transcript is a parsed test/golden/cmd_*.txt file.
type transcript struct {
	args        []string
	config      string
	requests    []string // as listed: "GET url -> file", "GET url -> (no data)", "mock path"
	stdout      string
	stderr      string
	logs        []string
	configAfter string // "" if the transcript has none
	exit        int
}

func parseTranscript(t *testing.T, path string) transcript {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tr transcript
	var section string
	lines := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "args: ") && section == "":
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "args: ")), &tr.args); err != nil {
				t.Fatalf("%s: args: %v", path, err)
			}
		case strings.HasPrefix(line, "config: ") && section == "":
			tr.config = strings.TrimPrefix(line, "config: ")
		case line == "-- requests --", line == "-- stdout --", line == "-- stderr --",
			line == "-- log (INFO and above) --", line == "-- config after (parsed, sorted) --":
			section = line
		case strings.HasPrefix(line, "-- exit ") && strings.HasSuffix(line, " --"):
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "-- exit "), " --"))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			tr.exit = n
			section = ""
		case section != "":
			lines[section] = append(lines[section], line)
		}
	}
	text := func(sec string) string {
		l := lines[sec]
		if len(l) == 1 && l[0] == "(empty)" {
			return ""
		}
		return strings.Join(l, "\n")
	}
	list := func(sec string) []string {
		l := lines[sec]
		if len(l) == 1 && (l[0] == "(none)") {
			return nil
		}
		return l
	}
	tr.requests = list("-- requests --")
	tr.stdout = text("-- stdout --")
	tr.stderr = text("-- stderr --")
	tr.logs = list("-- log (INFO and above) --")
	tr.configAfter = text("-- config after (parsed, sorted) --")
	return tr
}

// TestCommandTranscripts runs every test/golden/cmd_*.txt: the command, with
// the network replaced by the transcript's URL -> fixture table.
func TestCommandTranscripts(t *testing.T) {
	files, err := filepath.Glob(golden + "cmd_*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no transcripts found: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".txt")
		t.Run(name, func(t *testing.T) {
			tr := parseTranscript(t, file)
			h := newHarness(t)

			// The URL -> fixture table, from the requests the Python made.
			var want []string
			for _, r := range tr.requests {
				rest, ok := strings.CutPrefix(r, "GET ")
				if !ok {
					want = append(want, r) // "mock path"
					continue
				}
				url, fixture, _ := strings.Cut(rest, " -> ")
				want = append(want, "GET "+url)
				if fixture == "(no data)" {
					continue
				}
				body, err := os.ReadFile(fixtures + fixture)
				if err != nil {
					t.Fatal(err)
				}
				h.src.data[url] = string(body)
			}
			cfgDir := filepath.Join(h.home, ".config", "playcall")
			if err := os.MkdirAll(cfgDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(tr.config), 0o644); err != nil {
				t.Fatal(err)
			}

			// Transcripts name mock files relative to the repo root.
			args := append([]string(nil), tr.args...)
			for i, a := range args {
				if strings.HasPrefix(a, "test/fixtures/") {
					args[i] = "../../" + a
				}
			}
			code := h.run(t, args...)

			if code != tr.exit {
				t.Errorf("exit %d, want %d", code, tr.exit)
			}
			if got := strings.TrimRight(h.stdout.String(), "\n"); got != tr.stdout {
				t.Errorf("stdout:\n%s\nwant:\n%s", got, tr.stdout)
			}
			if got := strings.TrimRight(h.stderr.String(), "\n"); got != tr.stderr {
				t.Errorf("stderr:\n%s\nwant:\n%s", got, tr.stderr)
			}
			if !reflect.DeepEqual(h.logs, tr.logs) {
				t.Errorf("log:\n%q\nwant:\n%q", h.logs, tr.logs)
			}
			if !reflect.DeepEqual(h.src.requests, want) {
				t.Errorf("requests:\n%q\nwant:\n%q", h.src.requests, want)
			}
			if len(h.runner.calls) != 0 {
				t.Errorf("ran %v", h.runner.calls)
			}
			if tr.configAfter != "" {
				b, err := os.ReadFile(filepath.Join(cfgDir, "config.json"))
				if err != nil {
					t.Fatal(err)
				}
				var gotCfg, wantCfg any
				if err := json.Unmarshal(b, &gotCfg); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tr.configAfter), &wantCfg); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(gotCfg, wantCfg) {
					t.Errorf("config after:\n%s\nwant:\n%s", b, tr.configAfter)
				}
			}
		})
	}
}
