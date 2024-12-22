package utils

import (
	"crypto/md5"
	"sort"
	"strings"

	"github.com/gofrs/uuid"
)

func GenUuidFromStrings(uuids ...string) string {
	if len(uuids) == 0 {
		uuids = append(uuids, "00000000-0000-0000-0000-000000000000")
	}

	// Sort the UUIDs to ensure consistent ordering
	sortedUUIDs := make([]string, len(uuids))
	copy(sortedUUIDs, uuids)
	sort.Strings(sortedUUIDs)

	// Concatenate all sorted UUIDs
	concatenatedUUIDs := strings.Join(sortedUUIDs, "")

	return uuidHash([]byte(concatenatedUUIDs))
}

func uuidHash(b []byte) string {
	h := md5.New()

	h.Write(b)
	sum := h.Sum(nil)
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return uuid.FromBytesOrNil(sum).String()
}
