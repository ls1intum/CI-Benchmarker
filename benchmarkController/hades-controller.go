package benchmarkController

import (
	"log/slog"
	"net/http"

	"github.com/Hades-Scheduler/CI-Benchmarker/executor"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"
	"github.com/gin-gonic/gin"
)

// NewHadesDockerBenchmark builds the handler for POST /v1/benchmark/hades-docker.
//
// @Summary      Benchmark Hades with the Docker executor
// @Description  Submits jobs to a Hades instance running the Docker executor.
// @Tags         benchmark
// @Accept       json
// @Produce      json
// @Param        host         query  string  true   "Hades REST endpoint to submit to"
// @Param        count        query  int     false  "Number of jobs to submit"  default(1)
// @Param        run_id       query  string  false  "Run identifier; generated when omitted"
// @Param        concurrency  query  int     false  "Maximum in-flight submissions"
// @Param        rate         query  number  false  "Offered submissions per second"
// @Param        workload_id  query  string  false  "Identifier for the submitted payload"
// @Param        commit_hash  query  string  false  "Optional run tag"
// @Param        status_callback_url  query  string  false  "Where Hades should POST terminal status; defaults to CALLBACK_BASE_URL + /v1/callback"
// @Param        payload      body   payload.RESTPayload  true  "Job payload to submit"
// @Success      200  {object}  RunResponse
// @Failure      400  {object}  response.ErrorMessage
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /benchmark/hades-docker [post]
func NewHadesDockerBenchmark(store *persister.DBPersister, cfg config.Config) gin.HandlerFunc {
	return newHadesBenchmark(store, cfg, executor.Docker)
}

// NewHadesKubernetesBenchmark builds the handler for POST /v1/benchmark/hades-k8s.
//
// @Summary      Benchmark Hades with the Kubernetes executor
// @Description  Submits jobs to a Hades instance running the Kubernetes executor.
// @Tags         benchmark
// @Accept       json
// @Produce      json
// @Param        host         query  string  true   "Hades REST endpoint to submit to"
// @Param        count        query  int     false  "Number of jobs to submit"  default(1)
// @Param        run_id       query  string  false  "Run identifier; generated when omitted"
// @Param        concurrency  query  int     false  "Maximum in-flight submissions"
// @Param        rate         query  number  false  "Offered submissions per second"
// @Param        workload_id  query  string  false  "Identifier for the submitted payload"
// @Param        commit_hash  query  string  false  "Optional run tag"
// @Param        status_callback_url  query  string  false  "Where Hades should POST terminal status; defaults to CALLBACK_BASE_URL + /v1/callback"
// @Param        payload      body   payload.RESTPayload  true  "Job payload to submit"
// @Success      200  {object}  RunResponse
// @Failure      400  {object}  response.ErrorMessage
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /benchmark/hades-k8s [post]
func NewHadesKubernetesBenchmark(store *persister.DBPersister, cfg config.Config) gin.HandlerFunc {
	return newHadesBenchmark(store, cfg, executor.Kubernetes)
}

func newHadesBenchmark(store *persister.DBPersister, cfg config.Config, executorType executor.ExecutorType) gin.HandlerFunc {
	return func(c *gin.Context) {
		hadesHost := c.Query("host")
		if hadesHost == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'host' is required"})
			return
		}

		// Where Hades should report terminal status. The per-request override
		// exists so a run can point at a different receiver without a redeploy.
		statusCallbackURL := c.Query("status_callback_url")
		if statusCallbackURL == "" {
			statusCallbackURL = cfg.StatusCallbackURL()
		}
		if statusCallbackURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "no status callback target: set CALLBACK_BASE_URL or pass 'status_callback_url'. " +
					"Without it Hades cannot report completion and no job would ever be measured.",
			})
			return
		}

		slog.Debug("Creating new Hades benchmark",
			slog.String("type", string(executorType)), slog.String("host", hadesHost))

		benchmark := Benchmark{
			Executor:  executor.NewHadesExecutorWithCallback(hadesHost, executorType, statusCallbackURL),
			Persister: store,
			Config:    cfg,
		}

		benchmark.HandleFunc(c)
	}
}
