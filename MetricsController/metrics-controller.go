package MetricsController

import (
	"bytes"
	"github.com/Mtze/CI-Benchmarker/persister"
	_ "github.com/Mtze/CI-Benchmarker/shared/response"
	"github.com/Mtze/CI-Benchmarker/shared/utils"
	"github.com/gin-gonic/gin"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
	"log"
	"math"
	"net/http"
	"sort"
)

// MetricSummary describes percentile statistics of a metric.
//
// @Description Percentile and descriptive statistics for a metric (latency / build time).
type MetricSummary struct {
	Description string `json:"description" example:"Queue Latency Summary representing the time taken for jobs to be queued before execution with seconds as unit."`
	TotalJobs   int    `json:"total_jobs" example:"125"`
	Average     int64  `json:"average"    example:"12"`
	Median      int64  `json:"median"     example:"11"`
	Q25         int64  `json:"q25"        example:"8"`
	Q75         int64  `json:"q75"        example:"15"`
	Max         int64  `json:"max"        example:"40"`
	Min         int64  `json:"min"        example:"2"`
}

//------------------------------------------------------------------------------
// Histogram End-points (PNG image)
//------------------------------------------------------------------------------

// GetQueueLatencyHistogram godoc
//
// @Summary      Histogram of queue latency
// @Description  Returns a PNG histogram showing distribution of queue latency (seconds).
// @Tags         metrics
// @Produce      png
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {string}  binary  "PNG image"
// @Failure 	 400  {object} 	response.ErrorMessage
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /benchmark/queue_latency/histogram [get]
func GetQueueLatencyHistogram(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetQueueLatenciesInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching queue latencies:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch queue latencies"})
		return
	}

	renderPlotAsPNG(c, "Queue Latency Distribution", "Queue Latency (s)", "Frequency", latencies, 20)
}

// GetBuildTimeHistogram godoc
//
// @Summary      Histogram of build time
// @Description  Returns a PNG histogram showing distribution of build time (seconds).
// @Tags         metrics
// @Produce      png
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {string}  binary  "PNG image"
// @Failure 	 400  {object} 	response.ErrorMessage
// @Failure      500  {object}   response.ServerErrorMessage
// @Router       /benchmark/build_time/histogram [get]
func GetBuildTimeHistogram(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	buildTimes, err := p.GetBuildTimesInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching build times:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch build times"})
		return
	}

	renderPlotAsPNG(c, "Build Time Distribution", "Build Time (s)", "Frequency", buildTimes, 20)
}

// GetTotalLatencyHistogram godoc
//
// @Summary      Histogram of total latency
// @Description  Returns a PNG histogram showing distribution of total latency (seconds).
// @Tags         metrics
// @Produce      png
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {string}  binary  "PNG image"
// @Failure 	 400  {object} 	response.ErrorMessage
// @Failure      500  {object}   response.ServerErrorMessage
// @Router       /benchmark/latency/histogram [get]
func GetTotalLatencyHistogram(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetTotalLatenciesInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching total latencies:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch total latencies"})
		return
	}

	renderPlotAsPNG(c, "Total Latency Distribution", "Total Latency (s)", "Frequency", latencies, 20)
}

//------------------------------------------------------------------------------
// Metrics Summary End-points (JSON)
//------------------------------------------------------------------------------

// GetQueueLatencyMetrics godoc
//
// @Summary      Queue latency statistics
// @Description  Returns percentile & descriptive statistics for queue latency.
// @Tags         metrics
// @Produce      json
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {object}  MetricSummary
// @Failure      400  {object}   response.ErrorMessage
// @Failure      404  {object}   response.NotFoundMessage
// @Failure      500  {object}   response.ServerErrorMessage
// @Router       /benchmark/queue_latency/metrics [get]
func GetQueueLatencyMetrics(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetQueueLatencySummaryInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching queue latency summary:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch queue latency summary"})
		return
	}

	if len(latencies) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	summary := calculateSummary(latencies, "Queue Latency Summary representing the time taken for jobs to be queued before execution with seconds as unit.")
	c.JSON(http.StatusOK, summary)
}

// GetBuildTimeMetrics godoc
//
// @Summary      Build time statistics
// @Description  Returns percentile & descriptive statistics for build time.
// @Tags         metrics
// @Produce      json
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {object}  MetricSummary
// @Failure      400  {object}   response.ErrorMessage
// @Failure      404  {object}   response.NotFoundMessage
// @Failure      500  {object}   response.ServerErrorMessage
// @Router       /benchmark/build_time/metrics [get]
func GetBuildTimeMetrics(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	buildTimes, err := p.GetBuildTimeSummaryInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching build time summary:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch build time summary"})
		return
	}

	if len(buildTimes) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	summary := calculateSummary(buildTimes, "Build Time Summary representing the time taken for jobs to complete execution with seconds as unit.")
	c.JSON(http.StatusOK, summary)
}

// GetTotalLatencyMetrics godoc
//
// @Summary      Total latency statistics
// @Description  Returns percentile & descriptive statistics for total latency.
// @Tags         metrics
// @Produce      json
// @Param        from         query  string  false  "Start time (RFC3339)"
// @Param        to           query  string  false  "End time (RFC3339)"
// @Param        commit_hash  query  string  false  "Optional commit hash filter"
// @Success      200  {object}  MetricSummary
// @Failure      400  {object}   response.ErrorMessage
// @Failure      404  {object}   response.NotFoundMessage
// @Failure      500  {object}   response.ServerErrorMessage
// @Router       /benchmark/latency/metrics [get]
func GetTotalLatencyMetrics(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	var commitHash *string
	if hash := c.Query("commit_hash"); hash != "" {
		commitHash = &hash
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetTotalLatenciesSummaryInRange(from, to, commitHash)
	if err != nil {
		log.Println("Error fetching total latency summary:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch total latency summary"})
		return
	}

	if len(latencies) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	summary := calculateSummary(latencies, "Total Latency Summary representing the end-to-end time from job creation to job completion (seconds).")
	c.JSON(http.StatusOK, summary)
}

//------------------------------------------------------------------------------
// Helper functions (unchanged)
//------------------------------------------------------------------------------

func sum(data []int64) int64 {
	total := int64(0)
	for _, v := range data {
		total += v
	}
	return total
}

func renderPlotAsPNG(c *gin.Context, title, xLabel, yLabel string, data []int64, bins int) {
	if len(data) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data to plot"})
		return
	}

	values := make(plotter.Values, len(data))
	for i, val := range data {
		values[i] = float64(val)
	}

	pg := plot.New()
	pg.Title.Text = title
	pg.X.Label.Text = xLabel
	pg.Y.Label.Text = yLabel

	h, err := plotter.NewHist(values, bins)
	if err != nil {
		log.Println("Error creating histogram:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create histogram"})
		return
	}
	pg.Add(h)

	img := vgimg.New(500, 500)
	dc := draw.New(img)
	pg.Draw(dc)

	png := vgimg.PngCanvas{Canvas: img}
	buffer := new(bytes.Buffer)
	if _, err := png.WriteTo(buffer); err != nil {
		log.Println("Error writing PNG to buffer:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate image"})
		return
	}

	c.Header("Content-Type", "image/png")
	c.Status(http.StatusOK)
	c.Writer.Write(buffer.Bytes())
}

func calculateSummary(data []int64, description string) MetricSummary {
	n := len(data)
	sort.Slice(data, func(i, j int) bool { return data[i] < data[j] })

	average := sum(data) / int64(n)
	var median int64
	if n%2 == 1 {
		median = data[n/2]
	} else {
		median = (data[n/2-1] + data[n/2]) / 2
	}
	q25 := data[int(math.Floor(float64(n)/4))]
	q75 := data[int(math.Floor(float64(n)*3/4))]
	maxVal := data[n-1]
	minVal := data[0]

	return MetricSummary{
		Description: description,
		TotalJobs:   n,
		Average:     average,
		Median:      median,
		Q25:         q25,
		Q75:         q75,
		Max:         maxVal,
		Min:         minVal,
	}
}
