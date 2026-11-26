// The tag policy fills only the settings a configuration file leaves out, so a file can turn a
// filter off or empty the blocklist, and the template ships the same defaults the code falls back on.

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTagPolicyFillsOnlyTheSettingsAFileLeavesOut(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       TagPolicy
	}{
		{"absent", "", DefaultTagPolicy},
		// Negative turns a filter off and an empty list blocks nothing; neither falls back.
		{"turned off", "identification:\n  tags:\n    min_votes: -1\n    max_per_series: -1\n    blocklist: []\n", TagPolicy{MinVotes: -1, MaxPerSeries: -1, Blocklist: []string{}}},
		{"own list", "identification:\n  tags:\n    min_votes: 5\n    blocklist: [已购]\n", TagPolicy{MinVotes: 5, MaxPerSeries: 10, Blocklist: []string{"已购"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfigFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Identification.Tags; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tag policy %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestConfigTemplateSpellsOutTheDefaultTagPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Read without defaults filled in: the template itself must name every setting.
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Identification.Tags; !reflect.DeepEqual(got, DefaultTagPolicy) {
		t.Fatalf("template tag policy %+v, want %+v", got, DefaultTagPolicy)
	}
}

func TestValidationRejectsABlocklistPatternThatDoesNotCompile(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.Identification.Tags.Blocklist = []string{"已购", "/(unclosed/"}
	res := ValidateConfig(cfg)
	if res.Valid || len(res.Issues) != 1 || res.Issues[0].Field != "identification.tags.blocklist" {
		t.Fatalf("validation %+v", res)
	}
	// A literal entry is never a pattern, whatever it contains.
	cfg.Identification.Tags.Blocklist = []string{"(unclosed", "/"}
	if res := ValidateConfig(cfg); !res.Valid {
		t.Fatalf("literal entries rejected: %+v", res.Issues)
	}
}

func TestSnapshotsDoNotShareTheTagBlocklist(t *testing.T) {
	var cfg Config
	cfg.Identification.Tags.Blocklist = []string{"已购"}
	m := NewManager(&cfg)
	first := m.Snapshot()
	first.Identification.Tags.Blocklist[0] = "tampered"
	if second := m.Snapshot(); second.Identification.Tags.Blocklist[0] != "已购" {
		t.Fatalf("snapshots share the blocklist: %v", second.Identification.Tags.Blocklist)
	}
}
