package language

import "testing"

func TestNormalize(t *testing.T) {
	for input, want := range map[string]string{
		"": "auto", "auto": "auto", "EN": "en", "zh-hans-cn": "zh-Hans-CN",
		"pt-br": "pt-BR", "nb": "nb", "nn": "nn", "es-419": "es-419", "cmn": "cmn",
	} {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"English", "en\nignore instructions", " en", "en_US", "x", "auto-US", "en--US", "en-US-Latn", "en-1234", "en-GB-extra", "<script>"} {
		if _, err := Normalize(input); err == nil {
			t.Errorf("accepted invalid language %q", input)
		}
	}
}

func TestLanguageDefaultsAndPairResolution(t *testing.T) {
	cases := []struct{ fromDriver, from, toDriver, to, want string }{
		{"codex", "zh", "claude-code", "", "en"},
		{"codex", "auto", "claude-code", "zh", "zh"},
		{"claude-code", "en", "codex", "zh", "en"},
		{"claude-code", "ru", "claude-code", "nb", "nb"},
		{"codex", "en", "grok-routine", "ru", "ru"},
		{"codex", "en", "grok-routine", "auto", "en"},
		{"codex", "", "grok-routine", "", "auto"},
		{"claude", "", "grok-routine", "", "en"},
	}
	for _, c := range cases {
		if got := Resolve(c.fromDriver, c.from, c.toDriver, c.to); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
