// Package language resolves expression preferences without interpreting or
// translating message bodies. It grants no permissions and invokes no models.
package language

import (
	"errors"
	"regexp"
	"strings"
)

var tag = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z]{4})?(?:-(?:[A-Za-z]{2}|[0-9]{3}))?$`)

// Normalize supports a language with optional script and region. This is a
// bounded subset of language tags, not an assertion of model language support.
func Normalize(value string) (string, error) {
	if value == "" || strings.EqualFold(value, "auto") {
		return "auto", nil
	}
	if len(value) > 35 || !tag.MatchString(value) {
		return "", errors.New("communication_language must be auto or a language tag such as en, zh-Hans, nb or pt-BR")
	}
	parts := strings.Split(strings.ToLower(value), "-")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) == 4 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		} else {
			parts[i] = strings.ToUpper(parts[i])
		}
	}
	return strings.Join(parts, "-"), nil
}

func isClaude(driver string) bool {
	return strings.EqualFold(driver, "claude-code") || strings.EqualFold(driver, "claude")
}

// Effective returns the configured preference, including explicit Chinese.
// Invalid persisted hints fall back to defaults; write boundaries reject them.
func Effective(driver, configured string) string {
	value, err := Normalize(configured)
	if err == nil && value != "auto" {
		return value
	}
	if isClaude(driver) {
		return "en"
	}
	return "auto"
}

// Resolve lets a Claude endpoint choose its collaboration language in either
// direction. Otherwise the recipient's explicit preference wins. Human-facing
// language is deliberately outside this setting.
func Resolve(senderDriver, senderLanguage, recipientDriver, recipientLanguage string) string {
	from, to := Effective(senderDriver, senderLanguage), Effective(recipientDriver, recipientLanguage)
	if isClaude(recipientDriver) {
		return to
	}
	if isClaude(senderDriver) {
		return from
	}
	if to != "auto" {
		return to
	}
	return from
}
