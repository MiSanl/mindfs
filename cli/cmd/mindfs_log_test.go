package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMindFSLogFileWritesUTF8BOMOnFreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mindfs.log")
	f, err := openMindFSLogFile(path)
	if err != nil {
		t.Fatalf("openMindFSLogFile: %v", err)
	}
	_ = f.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Fatalf("expected UTF-8 BOM at start, got %v", raw)
	}

	// Re-open existing non-empty file must not write another BOM.
	f2, err := openMindFSLogFile(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_, _ = f2.WriteString("hello\n")
	_ = f2.Close()
	raw2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Only one BOM
	count := 0
	for i := 0; i+2 < len(raw2); i++ {
		if raw2[i] == 0xEF && raw2[i+1] == 0xBB && raw2[i+2] == 0xBF {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("BOM count = %d, want 1", count)
	}
}

func TestOpenMindFSLogFileWritesBOMOnEmptyExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openMindFSLogFile(path)
	if err != nil {
		t.Fatalf("openMindFSLogFile: %v", err)
	}
	_ = f.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Fatalf("empty existing file should get BOM, got %v", raw)
	}
}
