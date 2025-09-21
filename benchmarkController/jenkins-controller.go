package benchmarkController

import (
	"log/slog"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
)

func NewJenkinsBenchmark() gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Debug("Creating new Jenkins benchmark")

		jenkinsHost := c.Query("host")
		jenkinsUser := c.Query("user")
		jenkinsAPIToken := c.Query("api_token")
		jenkinsJobPath := c.Query("job_path")
		useParameters := c.DefaultQuery("use_parameters", "false") == "true"
		benchmark := Benchmark{
			Executor:  executor.NewJenkinsExecutor(jenkinsHost, jenkinsUser, jenkinsAPIToken, jenkinsJobPath, useParameters),
			Persister: persister.NewDBPersister(),
		}

		benchmark.HandleFunc(c)
	}
}
