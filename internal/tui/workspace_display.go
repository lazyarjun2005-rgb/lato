package tui

import "strings"

func truncateDisplay(value string, width int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	left := (width - 1) / 2
	right := width - 1 - left
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

func displayWorkspacePath(path string, width int) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" {
		return "workspace unavailable"
	}
	unc := strings.HasPrefix(path, "//")
	parts := strings.Split(path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			clean = append(clean, part)
		}
	}
	path = strings.Join(clean, "/")
	if unc {
		path = "//" + path
	}
	if len(clean) > 0 && len(clean[0]) == 2 && clean[0][1] == ':' {
		path = clean[0] + "/" + strings.Join(clean[1:], "/")
	}
	if strings.HasPrefix(path, "home/") {
		path = "/" + path
	}
	if width <= 0 || len([]rune(path)) <= width {
		return path
	}
	if len(clean) >= 3 {
		prefix := clean[0]
		if unc {
			prefix = "//" + prefix
		}
		path = prefix + "/…/" + clean[len(clean)-1]
	}
	return truncateDisplay(path, width)
}

func appendBoundedActivity(items []activityItem, item activityItem) []activityItem {
	items = append(items, item)
	if len(items) > maxLiveActivities {
		items = items[len(items)-maxLiveActivities:]
	}
	return items
}
