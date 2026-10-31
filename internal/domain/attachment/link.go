package attachment

import (
	"regexp"
	"slices"

	"github.com/google/uuid"
)

var urlIDRe = regexp.MustCompile(`/api/attachments/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/`)

// IDsInMarkdown returns the attachment ids of download links in md, in order,
// without duplicates.
func IDsInMarkdown(md string) []uuid.UUID {
	var out []uuid.UUID
	for _, m := range urlIDRe.FindAllStringSubmatch(md, -1) {
		id, err := uuid.Parse(m[1])
		if err == nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
