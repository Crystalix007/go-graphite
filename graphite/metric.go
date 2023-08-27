package graphite

import (
	"fmt"
	"strings"
	"time"
)

// MetricMetadata represents a metric that can be sent to a Graphite server.
type MetricMetadata struct {
	Name []string
	Tags map[string]string
}

// SubMetric returns a new metric metadata with the given name appended to the
// current metric's name. This allows child metrics to be created from a parent
// metric.
func (m MetricMetadata) SubMetric(name string, tags map[string]string) *MetricMetadata {
	return &MetricMetadata{
		Name: append(m.Name, strings.ToLower(name)),
		Tags: tags,
	}
}

// Metric represents a metric that has been queued for sending to the
// Graphite server.
type Metric struct {
	MetricMetadata
	Value     string
	Timestamp time.Time
}

// String returns the metric line for sending to Graphite.
func (m Metric) String() string {
	var metricString strings.Builder

	metricString.WriteString(m.Name[0])

	for _, n := range m.Name[1:] {
		metricString.WriteRune('.')
		metricString.WriteString(n)
	}

	for tag, value := range m.Tags {
		metricString.WriteRune(';')
		metricString.WriteString(tag)
		metricString.WriteRune('=')
		metricString.WriteString(value)
	}

	metricString.WriteRune(' ')
	metricString.WriteString(m.Value)

	metricString.WriteRune(' ')
	metricString.WriteString(fmt.Sprint(m.Timestamp.Unix()))

	return metricString.String()
}
