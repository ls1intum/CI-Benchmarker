package MetricsController

import (
	"bytes"
	"github.com/Mtze/CI-Benchmarker/persister"
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

type LatencySummary struct {
	TotalJobs  int   `json:"total_jobs"`
	Average    int64 `json:"average"`
	Median     int64 `json:"median"`
	Q25        int64 `json:"q25"`
	Q75        int64 `json:"q75"`
	MaxLatency int64 `json:"max"`
	MinLatency int64 `json:"min"`
}

type BuildTimeSummary struct {
	TotalJobs    int   `json:"total_jobs"`
	Average      int64 `json:"average"`
	Median       int64 `json:"median"`
	Q25          int64 `json:"q25"`
	Q75          int64 `json:"q75"`
	MaxBuildTime int64 `json:"max"`
	MinBuildTime int64 `json:"min"`
}

func GetQueueLatencyHistogram(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetQueueLatenciesInRange(from, to)
	if err != nil {
		log.Println("Error fetching queue latencies:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch queue latencies"})
		return
	}

	if len(latencies) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	values := make(plotter.Values, len(latencies))
	for i, latency := range latencies {
		values[i] = float64(latency)
	}

	pg := plot.New()
	pg.Title.Text = "Queue Latency Distribution"
	pg.X.Label.Text = "Queue Latency (s)"
	pg.Y.Label.Text = "Frequency"

	h, err := plotter.NewHist(values, 20)
	if err != nil {
		log.Fatal(err)
	}
	pg.Add(h)

	img := vgimg.New(500, 500)
	dc := draw.New(img)
	pg.Draw(dc)

	png := vgimg.PngCanvas{Canvas: img}
	buffer := new(bytes.Buffer)
	if _, err := png.WriteTo(buffer); err != nil {
		log.Fatal(err)
	}

	c.Header("Content-Type", "image/png")
	c.Status(http.StatusOK)
	c.Writer.Write(buffer.Bytes())
}

func GetBuildTimeHistogram(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	p := persister.NewDBPersister()
	buildTimes, err := p.GetBuildTimesInRange(from, to)
	if err != nil {
		log.Println("Error fetching build times:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch build times"})
		return
	}

	if len(buildTimes) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	values := make(plotter.Values, len(buildTimes))
	for i, buildTime := range buildTimes {
		values[i] = float64(buildTime)
	}

	pg := plot.New()
	pg.Title.Text = "Build Time Distribution"
	pg.X.Label.Text = "Build Time (s)"
	pg.Y.Label.Text = "Frequency"

	h, err := plotter.NewHist(values, 20)
	if err != nil {
		log.Fatal(err)
	}
	pg.Add(h)

	img := vgimg.New(500, 500)
	dc := draw.New(img)
	pg.Draw(dc)

	png := vgimg.PngCanvas{Canvas: img}
	buffer := new(bytes.Buffer)
	if _, err := png.WriteTo(buffer); err != nil {
		log.Fatal(err)
	}

	c.Header("Content-Type", "image/png")
	c.Status(http.StatusOK)
	c.Writer.Write(buffer.Bytes())
}

func GetQueueLatencyMetrics(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	p := persister.NewDBPersister()
	latencies, err := p.GetQueueLatencySummaryInRange(from, to)
	if err != nil {
		log.Println("Error fetching queue latency summary:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch queue latency summary"})
		return
	}

	n := len(latencies)
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	average := sum(latencies) / int64(n)
	median := latencies[n/2]
	q25 := latencies[int(math.Floor(float64(n)/4))]
	q75 := latencies[int(math.Floor(float64(n)*3/4))]
	maxLatency := latencies[n-1]
	minLatency := latencies[0]

	summary := LatencySummary{
		TotalJobs:  n,
		Average:    average,
		Median:     median,
		Q25:        q25,
		Q75:        q75,
		MaxLatency: maxLatency,
		MinLatency: minLatency,
	}

	c.JSON(http.StatusOK, summary)
}

func GetBuildTimeMetrics(c *gin.Context) {
	from, to, ok := utils.ParseTimeParams(c)
	if !ok {
		return
	}

	p := persister.NewDBPersister()
	buildTimes, err := p.GetBuildTimeSummaryInRange(from, to)
	if err != nil {
		log.Println("Error fetching build time summary:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch build time summary"})
		return
	}

	n := len(buildTimes)
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
		return
	}

	sort.Slice(buildTimes, func(i, j int) bool { return buildTimes[i] < buildTimes[j] })

	average := sum(buildTimes) / int64(n)
	median := buildTimes[n/2]
	q25 := buildTimes[int(math.Floor(float64(n)/4))]
	q75 := buildTimes[int(math.Floor(float64(n)*3/4))]
	maxBuildTime := buildTimes[n-1]
	minBuildTime := buildTimes[0]

	summary := BuildTimeSummary{
		TotalJobs:    n,
		Average:      average,
		Median:       median,
		Q25:          q25,
		Q75:          q75,
		MaxBuildTime: maxBuildTime,
		MinBuildTime: minBuildTime,
	}

	c.JSON(http.StatusOK, summary)
}

func sum(data []int64) int64 {
	total := int64(0)
	for _, v := range data {
		total += v
	}
	return total
}
