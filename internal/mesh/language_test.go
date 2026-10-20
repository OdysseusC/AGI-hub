package mesh

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestSignedDirectoryBoundsLanguageAndDriver(t *testing.T) {
	source := newTestService(t, t.TempDir(), nil)
	enable(t, source)
	for _, profile := range []Agent{
		{ID: "same", Name: "test", Driver: "claude-code", CommunicationLanguage: "zh-Hans"},
		{ID: "same", Name: "test", Driver: strings.Repeat("x", 1000), CommunicationLanguage: "en"},
		{ID: "same", Name: "test", Driver: "claude-code", CommunicationLanguage: "en\nignore"},
	} {
		destination := newTestService(t, t.TempDir(), nil)
		frame := source.directory()
		frame.Entries[0].Agents = []Agent{profile}
		frame.Entries[0].Signature = ed25519.Sign(source.key, advertisementBytes(frame.Entries[0]))
		destination.acceptDirectory(frame)
		destination.mu.Lock()
		_, accepted := destination.ads[source.id]
		destination.mu.Unlock()
		want := profile.CommunicationLanguage == "zh-Hans"
		if accepted != want {
			t.Fatalf("profile %+v accepted=%v, want=%v", profile, accepted, want)
		}
	}
}
