package deepseektools

import (
	"regexp"
	"strings"
)

var (
	nativeHeading    = regexp.MustCompile(`^(.+?)\s+\((https?://[^\s]+)\)\s*$`)
	markdownHeading  = regexp.MustCompile(`^\[([^\]]+)\]\((https?://[^\s]+)\)\s*$`)
	nativeRef        = regexp.MustCompile(`【([^】]+)】`)
	nativeLine       = regexp.MustCompile(`^L[0-9]+:\s?`)
	readableCitation = regexp.MustCompile(`(?:【[^】]*】|[^]*)`)
	clippedText      = regexp.MustCompile(`(?i)(?:\[truncated\]|<truncated>|<response clipped>|\.\.\.\s*\(truncated\)|output (?:is |was )?truncated|content (?:is |was )?truncated)`)
)

// parseNative recognizes independently attributed title/URL blocks, not arbitrary
// model prose. Metadata and citation tokens are not readable page content.
func parseNative(text string) (sources []source, recognized, failed, clipped bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var current *source
	var body []string
	flush := func() {
		if current == nil {
			return
		}
		content := strings.TrimSpace(strings.Join(body, "\n"))
		if looksError(current.title) || looksError(content) {
			failed = true
		} else if validateURL(current.url) == nil {
			current.content = content
			current.truncated = current.truncated || clippedText.MatchString(content)
			if len(sources) < maxSources {
				sources = append(sources, *current)
			} else {
				clipped = true
			}
		} else {
			clipped = true
		}
		current = nil
		body = nil
	}
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		heading := strings.TrimLeft(trim, "# ")
		match := nativeHeading.FindStringSubmatch(heading)
		if match == nil {
			match = markdownHeading.FindStringSubmatch(heading)
		}
		// Numbered page lines may quote titles/URLs; they remain inside their page.
		if match != nil && !nativeLine.MatchString(trim) {
			flush()
			recognized = true
			current = &source{title: strings.TrimSpace(match[1]), url: match[2]}
			continue
		}
		if current == nil {
			continue
		}
		if ref := nativeRef.FindStringSubmatch(trim); ref != nil && current.ref == "" {
			current.ref = ref[1]
		}
		clean := strings.TrimSpace(readableCitation.ReplaceAllString(trim, ""))
		low := strings.ToLower(clean)
		if strings.HasPrefix(low, "[wordlim") || strings.HasPrefix(low, "content type:") || strings.HasPrefix(low, "source: open(") || strings.HasPrefix(low, "source: search(") || strings.HasPrefix(low, "crawled:") || strings.HasPrefix(low, "published:") || strings.HasPrefix(low, "total lines:") {
			continue
		}
		if clippedText.MatchString(clean) {
			current.truncated = true
		}
		if nativeLine.MatchString(trim) {
			clean = nativeLine.ReplaceAllString(trim, "")
			clean = readableCitation.ReplaceAllString(clean, "")
		}
		if strings.TrimSpace(clean) == "" && trim != "" && !nativeLine.MatchString(trim) {
			continue
		}
		body = append(body, clean)
	}
	flush()
	return
}
