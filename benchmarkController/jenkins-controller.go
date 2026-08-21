package benchmarkController

import (
	"log/slog"
	"net/http"

	"github.com/Hades-Scheduler/CI-Benchmarker/executor"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"
	"github.com/gin-gonic/gin"
)

// NewJenkinsBenchmark builds the handler for POST /v1/benchmark/jenkins.
//
// @Summary      Benchmark Jenkins
// @Description  Triggers Jenkins builds. The benchmarker generates the job id and passes it as the HADES_UUID build parameter, which the Jenkins post-build notification returns in build.parameters so the callback can be matched.
// @Tags         benchmark
// @Accept       json
// @Produce      json
// @Param        host           query  string  true   "Jenkins base URL"
// @Param        user           query  string  true   "Jenkins user"
// @Param        api_token      query  string  true   "Jenkins API token"
// @Param        job_path       query  string  true   "Path to the Jenkins job"
// @Param        use_parameters query  bool    false  "Trigger with build parameters"  default(false)
// @Param        count          query  int     false  "Number of jobs to submit"  default(1)
// @Param        run_id         query  string  false  "Run identifier; generated when omitted"
// @Param        concurrency    query  int     false  "Maximum in-flight submissions"
// @Param        rate           query  number  false  "Offered submissions per second"
// @Param        workload_id    query  string  false  "Identifier for the submitted payload"
// @Param        commit_hash    query  string  false  "Optional run tag"
// @Param        payload        body   payload.RESTPayload  true  "Job payload to submit"
// @Success      200  {object}  RunResponse
// @Failure      400  {object}  response.ErrorMessage
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /benchmark/jenkins [post]
func NewJenkinsBenchmark(store *persister.DBPersister, cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		jenkinsHost := c.Query("host")
		jenkinsUser := c.Query("user")
		jenkinsAPIToken := c.Query("api_token")
		jenkinsJobPath := c.Query("job_path")
		useParameters := c.DefaultQuery("use_parameters", "false") == "true"

		if jenkinsHost == "" || jenkinsUser == "" || jenkinsAPIToken == "" || jenkinsJobPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "query parameters 'host', 'user', 'api_token' and 'job_path' are required",
			})
			return
		}

		slog.Debug("Creating new Jenkins benchmark", slog.String("host", jenkinsHost))

		benchmark := Benchmark{
			Executor:  executor.NewJenkinsExecutor(jenkinsHost, jenkinsUser, jenkinsAPIToken, jenkinsJobPath, useParameters),
			Persister: store,
			Config:    cfg,
		}

		benchmark.HandleFunc(c)
	}
}
