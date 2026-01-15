package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestServerRun(t *testing.T) {
	// import "./docs"
	// docs.SwaggerInfo.Title = "Swagger Example API"
	// docs.SwaggerInfo.Description = "This is a sample server Petstore server."
	// docs.SwaggerInfo.Version = "1.0"
	// docs.SwaggerInfo.Host = "petstore.swagger.io"
	// docs.SwaggerInfo.BasePath = "/v2"
	// docs.SwaggerInfo.Schemes = []string{"http", "https"}

	gin.SetMode(gin.ReleaseMode)
	s := NewServer(
		"",
		gin.New(),
		WithSwagOptions(SwagOption{Enabled: true}),
		WithMetricsOptions(MetricsOption{Enabled: true}),
		WithHealthOptions(HealthOption{Enabled: true}),
	)

	go func() {
		err := s.Run()
		assert.NoError(t, err)
	}()

	time.Sleep(10 * time.Second) // give server time to start

	resp, err := http.Get("http://localhost:8080")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
