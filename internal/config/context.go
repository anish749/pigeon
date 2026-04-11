package config

import "gopkg.in/yaml.v3"

// Context maps platform keys ("gws", "slack", "whatsapp", "linear") to
// account identifiers within that platform. Values can be a single string
// or a list of strings in YAML — StringOrSlice handles both.
//
// Account identifiers are matched against configured accounts:
//   - gws: email address (GWSConfig.Email)
//   - slack: workspace name (SlackConfig.Workspace)
//   - whatsapp: phone number (extracted from WhatsAppConfig.DeviceJID)
//   - linear: workspace slug (LinearConfig.Workspace)
type Context map[string]StringOrSlice

// Accounts returns the account identifiers for a given platform key.
// Returns nil if the platform is not present in this context.
func (c Context) Accounts(platform string) []string {
	if c == nil {
		return nil
	}
	return c[platform]
}

// StringOrSlice is a YAML type that accepts either a single string or an
// array of strings. When unmarshaled from a scalar, it becomes a
// single-element slice. When marshaled, a single-element slice is written
// as a scalar for cleaner YAML output.
type StringOrSlice []string

func (s *StringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = StringOrSlice{value.Value}
		return nil
	}
	var ss []string
	if err := value.Decode(&ss); err != nil {
		return err
	}
	*s = ss
	return nil
}

func (s StringOrSlice) MarshalYAML() (any, error) {
	if len(s) == 1 {
		return s[0], nil
	}
	return []string(s), nil
}
