package nexttask

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

var modelNames = map[string]string{
	"claude-opus-5-5":   "Opus 5.5",
	"claude-sonnet-5-5": "Sonnet 5.5",
	"claude-fable-5-1":  "Fable 5.1",
}

func modelName(id string) string {
	if n, ok := modelNames[id]; ok {
		return n
	}
	return id
}

// formatAge is "just now", "12 min ago", "3 h ago" or "2 d ago".
func formatAge(then, now time.Time) string {
	minutes := int(now.Sub(then) / time.Minute)
	switch {
	case minutes < 1:
		return "just now"
	case minutes < 60:
		return fmt.Sprintf("%d min ago", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%d h ago", minutes/60)
	}
	return fmt.Sprintf("%d d ago", minutes/60/24)
}

// formatElapsed is m:ss, for the progress row.
func formatElapsed(d time.Duration) string {
	s := max(0, int(d/time.Second))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// formatTimeRange is "16:00–16:25" in local time.
func formatTimeRange(start, end time.Time) string {
	return start.Local().Format("15:04") + "–" + end.Local().Format("15:04")
}

// formatStartsIn is "in 9 min", "in 1 h 20 min", "now" or "started 3 min ago".
func formatStartsIn(start, now time.Time) string {
	minutes := int(math.Round(start.Sub(now).Minutes()))
	switch {
	case minutes == 0:
		return "now"
	case minutes < 0:
		return fmt.Sprintf("started %d min ago", -minutes)
	case minutes < 60:
		return fmt.Sprintf("in %d min", minutes)
	}
	s := fmt.Sprintf("in %d h", minutes/60)
	if rest := minutes % 60; rest > 0 {
		s += fmt.Sprintf(" %d min", rest)
	}
	return s
}

func isLink(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// appFor names the app a link opens — Jira, Slack, Gmail or Calendar — so
// the label says where enter takes you; "" if we can't tell. It replaces the
// Raycast extension's brand icons.
func appFor(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	switch {
	case strings.HasSuffix(host, ".atlassian.net"):
		return "Jira"
	case host == "slack.com" || strings.HasSuffix(host, ".slack.com"):
		return "Slack"
	case host == "mail.google.com":
		return "Gmail"
	case host == "calendar.google.com":
		return "Calendar"
	}
	return ""
}

// taskApp judges by the link first (a BAU item can be a Jira ticket or a
// Slack canvas), then by the source.
func taskApp(t task) string {
	if a := appFor(t.URL); a != "" {
		return a
	}
	switch t.Source {
	case "slack":
		return "Slack"
	case "email":
		return "Gmail"
	case "calendar":
		return "Calendar"
	}
	return "Jira"
}
