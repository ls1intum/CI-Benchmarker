package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// Every endpoint using these parameters documents them as RFC3339, but the
// parser only accepted "2006-01-02T15:04:05", which is not RFC3339: it has no
// offset and no fractional seconds. A caller who followed the documentation got
// a 400.
func TestParseTimeAcceptsRFC3339(t *testing.T) {
	cases := map[string]time.Time{
		"2026-08-20T10:00:00Z":           time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		"2026-08-20T10:00:00.123456789Z": time.Date(2026, 8, 20, 10, 0, 0, 123456789, time.UTC),
		"2026-08-20T12:00:00+02:00":      time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got, err := ParseTime(input)
			if err != nil {
				t.Fatalf("ParseTime(%q): %v", input, err)
			}
			if got == nil {
				t.Fatal("ParseTime returned nil without an error")
			}
			if !got.Equal(want) {
				t.Errorf("ParseTime(%q) = %v, want %v", input, got, want)
			}
		})
	}
}

// The previously accepted layouts must keep working so existing dashboards and
// saved requests do not break.
func TestParseTimeAcceptsLegacyLayouts(t *testing.T) {
	for _, input := range []string{
		"2026-08-20T10:00:00",
		"2026-08-20 10:00:00",
		"2026-08-20",
	} {
		if _, err := ParseTime(input); err != nil {
			t.Errorf("ParseTime(%q): %v", input, err)
		}
	}
}

// An empty parameter means "unbounded", not "invalid".
func TestParseTimeEmptyIsUnbounded(t *testing.T) {
	got, err := ParseTime("")
	if err != nil {
		t.Fatalf("ParseTime(\"\"): %v", err)
	}
	if got != nil {
		t.Errorf("ParseTime(\"\") = %v, want nil", got)
	}
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	for _, input := range []string{"yesterday", "20/08/2026", "10:00:00"} {
		if _, err := ParseTime(input); err == nil {
			t.Errorf("ParseTime(%q) accepted an unparseable value", input)
		}
	}
}

func TestParseTimeParamsWritesBadRequest(t *testing.T) {
	router := gin.New()
	router.GET("/x", func(c *gin.Context) {
		if _, _, ok := ParseTimeParams(c); ok {
			c.Status(http.StatusOK)
		}
	})

	t.Run("rfc3339 accepted", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x?from=2026-08-20T10:00:00Z", nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; RFC3339 is what the API documents", recorder.Code)
		}
	})

	t.Run("garbage rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x?to=not-a-time", nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", recorder.Code)
		}
	})
}
