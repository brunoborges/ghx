package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPrintFormattedStatsCacheBudget(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want []string
	}{
		{
			name: "byte budget diagnostics",
			data: `{"cache_size":2,"max_cache_size":10,"cache_bytes":1234,"max_cache_bytes":4096,"cache_rejected":3}`,
			want: []string{
				"Cache Size:      2 / 10 entries",
				"Cache Bytes:     1234 / 4096 bytes",
				"Cache Rejected:  3",
			},
		},
		{
			name: "aggregate miss and removal reasons",
			data: `{"miss_reasons":{"bypass":2,"expired":3,"absent":4,"unspecified":1},"removal_reasons":{"expired":3,"capacity":5,"invalidated":6,"flushed":7}}`,
			want: []string{"Miss Reasons:    bypass=2 expired=3 absent=4 unspecified=1", "Entry Removals:  expired=3 capacity=5 invalidated=6 flushed=7"},
		},
		{
			name: "legacy stats without byte fields",
			data: `{"cache_size":2,"max_cache_size":10}`,
			want: []string{
				"Cache Size:      2 / 10 entries",
				"Cache Bytes:     0 / 0 bytes",
				"Cache Rejected:  0",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := os.CreateTemp(t.TempDir(), "stats")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := output.Close(); err != nil {
					t.Error(err)
				}
			})
			func() {
				original := os.Stdout
				os.Stdout = output
				defer func() { os.Stdout = original }()
				printFormattedStats([]byte(tt.data))
			}()
			data, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(data), "\n")
			for _, want := range tt.want {
				if !slices.Contains(lines, want) {
					t.Errorf("missing line %q in output:\n%s", want, data)
				}
			}
		})
	}
}
