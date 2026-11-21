package networks

// Exported field validators, so listings imported from peers go through
// exactly the same checks as local registrations (internal/services/federation).

// CleanName validates and trims a listing name.
func CleanName(v string) (string, error) { return cleanName(v) }

// CleanDescription validates and trims a description.
func CleanDescription(v string) (string, error) { return cleanDescription(v) }

// CleanTags validates, lowercases and de-duplicates tags.
func CleanTags(in []string) ([]string, error) { return cleanTags(in) }

// CleanLanguages validates and de-duplicates language codes.
func CleanLanguages(in []string) ([]string, error) { return cleanLanguages(in) }

// CleanURL returns nil for empty, else requires an https URL.
func CleanURL(v *string, name string) (*string, error) { return cleanURL(v, name) }

// CleanChannels validates channel names (ValidChannel), at most MaxChannels,
// de-duplicated ignoring ASCII case. field names the list in errors.
func CleanChannels(in []string, field string) ([]string, error) { return cleanChannels(in, field) }
