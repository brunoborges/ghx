package ghcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsShim_MarkerScript(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(shimPath, []byte(ShimContent()), 0755); err != nil {
		t.Fatal(err)
	}

	if !IsShim(shimPath, "") {
		t.Error("expected shim script to be detected as shim")
	}
}

func TestIsShim_RealBinaryELF(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	// ELF magic header
	if err := os.WriteFile(ghPath, []byte{0x7f, 'E', 'L', 'F', 0, 0, 0, 0}, 0755); err != nil {
		t.Fatal(err)
	}

	if IsShim(ghPath, "") {
		t.Error("expected ELF binary to NOT be detected as shim")
	}
}

func TestIsShim_RealBinaryMachO(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	// Mach-O 64-bit magic
	if err := os.WriteFile(ghPath, []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0755); err != nil {
		t.Fatal(err)
	}

	if IsShim(ghPath, "") {
		t.Error("expected Mach-O binary to NOT be detected as shim")
	}
}

func TestIsShim_Symlink(t *testing.T) {
	dir := t.TempDir()
	ghxBin := filepath.Join(dir, "ghx")
	if err := os.WriteFile(ghxBin, []byte("ghx-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	ghLink := filepath.Join(dir, "gh")
	if err := os.Symlink(ghxBin, ghLink); err != nil {
		t.Fatal(err)
	}

	if !IsShim(ghLink, ghxBin) {
		t.Error("expected symlink to ghx to be detected as shim")
	}
}

func TestIsShim_NonexistentFile(t *testing.T) {
	if IsShim("/nonexistent/path", "") {
		t.Error("expected nonexistent file to NOT be detected as shim")
	}
}

func TestIsShim_RegularScript_NoMarker(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if IsShim(scriptPath, "") {
		t.Error("expected script without marker to NOT be detected as shim")
	}
}

func TestShimContent(t *testing.T) {
	content := ShimContent()
	if content == "" {
		t.Fatal("expected non-empty shim content")
	}
	if !containsString(content, "#!/bin/sh") {
		t.Error("shim missing shebang")
	}
	if !containsString(content, ShimMarker) {
		t.Error("shim missing marker")
	}
	if !containsString(content, "exec ghx") {
		t.Error("shim missing exec ghx")
	}
}

func TestIsTextHeader(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		isText bool
	}{
		{"empty", []byte{}, true},
		{"short", []byte("hi"), true},
		{"shebang", []byte("#!/bin/sh\n"), true},
		{"elf", []byte{0x7f, 'E', 'L', 'F'}, false},
		{"macho64", []byte{0xcf, 0xfa, 0xed, 0xfe}, false},
		{"macho32", []byte{0xce, 0xfa, 0xed, 0xfe}, false},
		{"machoFat", []byte{0xca, 0xfe, 0xba, 0xbe}, false},
		{"pe", []byte{'M', 'Z', 0, 0}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTextHeader(tt.data)
			if got != tt.isText {
				t.Errorf("isTextHeader(%q) = %v, want %v", tt.name, got, tt.isText)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
