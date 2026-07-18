package authenv

import (
	"os"
	"slices"
	"testing"
)

func TestApplyReplacesInheritedAuthEnvironment(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"GH_TOKEN=stale-token",
		"GITHUB_TOKEN=also-stale",
		"XDG_CONFIG_HOME=/stale/config",
		"UNRELATED=value",
	}
	got := Apply(base, Environment{
		"GH_TOKEN":        "current-token",
		"XDG_CONFIG_HOME": "/current/config",
		"NOT_ALLOWED":     "ignored",
	})

	want := []string{
		"PATH=/usr/bin",
		"UNRELATED=value",
		"GH_TOKEN=current-token",
		"XDG_CONFIG_HOME=/current/config",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Apply() = %v, want %v", got, want)
	}
}

func TestCaptureIncludesOnlyCurrentGitHubAuthEnvironment(t *testing.T) {
	for _, name := range names {
		t.Setenv(name, "")
	}
	t.Setenv("GH_TOKEN", "current-token")
	t.Setenv("GH_HOST", "example.com")
	t.Setenv("UNRELATED_SECRET", "must-not-be-captured")

	got := Capture()
	if len(got) != 2 || got["GH_TOKEN"] != "current-token" || got["GH_HOST"] != "example.com" {
		t.Fatalf("Capture() = %v", got)
	}
	if _, ok := got["UNRELATED_SECRET"]; ok {
		t.Fatal("Capture() included an unrelated environment variable")
	}

	if err := os.Unsetenv("GH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Capture()["GH_TOKEN"]; ok {
		t.Fatal("Capture() included an unset token")
	}
}
