package dashboard

import "strings"

// isURL returns true if the string looks like a URL (http, https, or youtube shortlink).
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "youtu")
}
