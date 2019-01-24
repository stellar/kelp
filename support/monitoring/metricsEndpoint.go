package monitoring

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/interstellar/kelp/support/logger"

	"github.com/interstellar/kelp/support/networking"
)

// metricsEndpoint represents a monitoring API endpoint that always responds with a JSON
// encoding of the provided metrics. The auth level for the endpoint can be NoAuth (public access)
// or GoogleAuth which uses a Google account for authorization.
type metricsEndpoint struct {
	path      string
	metrics   Metrics
	authLevel networking.AuthLevel
	l         logger.Logger
}

// MakeMetricsEndpoint creates an Endpoint for the monitoring server with the desired auth level.
// The endpoint's response is always a JSON dump of the provided metrics.
func MakeMetricsEndpoint(path string, metrics Metrics, authLevel networking.AuthLevel, l logger.Logger) (networking.Endpoint, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("endpoint path must begin with /")
	}
	l := logger.MakeBasicLogger()
	s := &metricsEndpoint{
		path:      path,
		metrics:   metrics,
		authLevel: authLevel,
		l:         l,
	}
	return s, nil
}

func (m *metricsEndpoint) GetAuthLevel() networking.AuthLevel {
	return m.authLevel
}

func (m *metricsEndpoint) GetPath() string {
	return m.path
}

// GetHandlerFunc returns a HandlerFunc that writes the JSON representation of the metrics
// that's passed into the endpoint.
func (m *metricsEndpoint) GetHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json, e := m.metrics.MarshalJSON()
		if e != nil {
			m.l.Infof("error marshalling metrics json: %s\n", e)
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		_, e = w.Write(json)
		if e != nil {
			m.l.Infof("error writing to the response writer: %s\n", e)
		}
	}
}
