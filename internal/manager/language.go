package manager

import "agihub/internal/language"

func normalizeEndpointLanguage(endpoint *Endpoint) error {
	value, err := language.Normalize(endpoint.CommunicationLanguage)
	if err == nil && endpoint.CommunicationLanguage != "" {
		endpoint.CommunicationLanguage = value
	}
	return err
}

func collaborationLanguage(from, to Endpoint) string {
	return language.Resolve(from.Driver, from.CommunicationLanguage, to.Driver, to.CommunicationLanguage)
}
