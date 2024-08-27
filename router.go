package main

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var persister Persister

func startRouter() *gin.Engine {
	// Create a db instance - ugly hack - should be refactored
	slog.Debug("Creating DB persister")
	persister = NewDBPersister()

	slog.Debug("Setting up router")
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	version := r.Group("/v1")

	// Define the route for the result
	slog.Debug("Setting up routes")
	version.POST("/result", handleResult)
	version.POST("/benchmark/hades", benchmarkHades)

	return r
}

func benchmarkHades(c *gin.Context) {
	slog.Debug("Received request to start Hades test")
	// Get number of jobs to run
	countStr := c.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		slog.Error("Failed to parse count", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse count"})
		return
	}

	// Run the Hades test
	slog.Debug("Running Hades jobs", slog.Any("count", count))
	HadesTest(count)

	c.JSON(200, gin.H{"message": "Hades test started"})

}

// handleResult is a function to handle the result of a job
// The last container in the pipeline will send the result to this endpoint
func handleResult(c *gin.Context) {
	slog.Debug("Received result", slog.Any("result", c.Request.Body))

	uuid := uuid.New() //TODO: Get the UUID from the request
	time := time.Now()

	persister.StoreResult(uuid, time)

	c.JSON(200, gin.H{"message": "Result received"})
}
