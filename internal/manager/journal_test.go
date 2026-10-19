package manager

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestJournalRecoveryRestoresAppendHandleAfterRepair(t *testing.T) {
	for _, tailKind := range []string{"partial-json", "valid-json-without-newline"} {
		t.Run(tailKind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, journalName)
			file, _, _, err := openJournal(dir)
			if err != nil {
				t.Fatal(err)
			}
			first := emptySnapshot()
			first.Leader = "committed"
			if err = appendSnapshot(file, first); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			prefix, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tail := []byte(`{"snapshot":`)
			if tailKind == "valid-json-without-newline" {
				tail = bytes.TrimSuffix(prefix, []byte{'\n'})
			}
			if err = os.WriteFile(path, append(append([]byte(nil), prefix...), tail...), 0600); err != nil {
				t.Fatal(err)
			}
			file, state, _, err := openJournal(dir)
			if err != nil {
				t.Fatalf("recover incomplete tail with writable recovery handle: %v", err)
			}
			if state.Leader != "committed" {
				file.Close()
				t.Fatal("uncommitted tail replaced saved snapshot")
			}
			after := emptySnapshot()
			after.Leader = "appended-after-repair"
			if err = appendSnapshot(file, after); err != nil {
				file.Close()
				t.Fatalf("append after repair: %v", err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(saved, prefix) || bytes.Count(saved, []byte{'\n'}) != 2 {
				t.Fatal("post-repair append overwrote committed prefix or retained torn tail")
			}
			file, state, _, err = openJournal(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			if state.Leader != after.Leader {
				t.Fatal("post-repair append was not durable on reopen")
			}
		})
	}
}
