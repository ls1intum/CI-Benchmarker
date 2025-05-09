package MetricsController

import (
	"bytes"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
	"log"
	"net/http"
)

func GetQueueLatencyMetrics(c *gin.Context) {
	p := persister.NewDBPersister()

	latencies, err := p.GetQueueLatencies()
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
	p := persister.NewDBPersister()

	buildTimes, err := p.GetBuildTimes()
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

	h, err := plotter.NewHist(values, 20) // 20 个区间
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
