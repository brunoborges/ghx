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
