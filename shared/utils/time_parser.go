package utils

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// acceptedTimeLayouts are tried in order.
//
// The previous implementation accepted only "2006-01-02T15:04:05", which
// rejects real RFC3339 (it has no offset and no fractional seconds) even though
// every Swagger annotation on the endpoints using it promises RFC3339. Any
// caller that followed the documentation got a 400.
var acceptedTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseTime parses a timestamp in any accepted layout. An empty string yields
// a nil time with no error, meaning "unbounded".
func ParseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	for _, layout := range acceptedTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}

	return nil, fmt.Errorf("cannot parse %q; expected RFC3339, e.g. 2026-08-20T10:00:00Z", value)
}

// ParseTimeParams reads the optional from/to query parameters. It writes a 400
// and returns false when either is unparseable.
func ParseTimeParams(c *gin.Context) (*time.Time, *time.Time, bool) {
	from, err := ParseTime(c.Query("from"))
	if err != nil {
		slog.Warn("Invalid 'from' parameter", slog.Any("error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'from' parameter: " + err.Error()})
		return nil, nil, false
	}

	to, err := ParseTime(c.Query("to"))
	if err != nil {
		slog.Warn("Invalid 'to' parameter", slog.Any("error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'to' parameter: " + err.Error()})
		return nil, nil, false
	}

	return from, to, true
}
