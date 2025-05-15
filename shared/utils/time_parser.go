package utils

import (
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"time"
)

func parseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func ParseTimeParams(c *gin.Context) (*time.Time, *time.Time, bool) {
	fromStr := c.Query("from")
	toStr := c.Query("to")

	from, err := parseTime(fromStr)
	if err != nil {
		log.Println("Invalid 'from' parameter:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'from' parameter"})
		return nil, nil, false
	}

	to, err := parseTime(toStr)
	if err != nil {
		log.Println("Invalid 'to' parameter:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'to' parameter"})
		return nil, nil, false
	}

	return from, to, true
}
