package graphite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const (
	// DefaultMaxBufferSize is the default maximum number of metrics to buffer
	// before sending them to the Graphite server.
	DefaultMaxBufferSize = 1000

	// DefaultMaxMetricsPerMessage is the default maximum number of metrics to
	// send to the Graphite server in a single request.
	DefaultMaxMetricsPerMessage = 1

	// DefaultMaxTries is the default maximum number of times to try sending
	// metrics to the Graphite server.
	DefaultMaxTries = 3
)

var (
	// ErrNoAddress is returned when no server address is specified for the
	// Graphite client.
	ErrNoAddress = errors.New(
		"graphite: no address specified for Graphite server",
	)
)

// Client is the interface defining a Graphite client.
type Client interface {
	// SendMetric sends a metric to the Graphite server.
	SendMetric(ctx context.Context,
		metric MetricMetadata,
		value string,
		timestamp time.Time,
	) error

	// Submit submits the queued metrics to the Graphite server.
	Submit(ctx context.Context) error
}

type clientOptions struct {
	// The maximum number of metrics to buffer before sending them to the
	// Graphite server.
	MaxBufferSize int

	// The maximum number of metrics to send to the Graphite server in a single
	// request.
	MaxMetricsPerMessage int

	// The maximum number of times to retry sending metrics to the Graphite
	// server.
	MaxTries int

	// The connection to use when sending metrics to the Graphite server.
	Conn net.Conn

	// The address of the Graphite server. This is used if [Conn] is not set.
	Addr string
}

// ClientOption represents an option that can be set on the Graphite client.
type ClientOption func(c *clientOptions)

// WithMaxBufferSize sets the maximum number of metrics to buffer before
// sending them to the Graphite server.
func WithMaxBufferSize(maxBufferSize int) ClientOption {
	return func(c *clientOptions) {
		c.MaxBufferSize = maxBufferSize
	}
}

// WithMaxMetricsPerMessage sets the maximum number of metrics to send to the
// Graphite server in a single request.
func WithMaxMetricsPerMessage(maxMetricsPerMessage int) ClientOption {
	return func(c *clientOptions) {
		c.MaxMetricsPerMessage = maxMetricsPerMessage
	}
}

// WithMaxTries sets the maximum number of times to retry sending metrics to
// the Graphite server.
func WithMaxTries(maxTries int) ClientOption {
	return func(c *clientOptions) {
		c.MaxTries = maxTries
	}
}

// WithConnection sets the connection to use when sending metrics to the
// Graphite server.
func WithConnection(conn net.Conn) ClientOption {
	return func(c *clientOptions) {
		c.Conn = conn
	}
}

// WithAddress sets the address of the Graphite server. This is used if
// [Conn] is not set.
func WithAddress(addr string) ClientOption {
	return func(c *clientOptions) {
		c.Addr = addr
	}
}

func (c *clientOptions) setDefaults(ctx context.Context) error {
	if c.MaxBufferSize == 0 {
		c.MaxBufferSize = DefaultMaxBufferSize
	}

	if c.MaxMetricsPerMessage == 0 {
		c.MaxMetricsPerMessage = DefaultMaxMetricsPerMessage
	}

	if c.MaxTries == 0 {
		c.MaxTries = DefaultMaxTries
	}

	if c.Conn == nil && c.Addr == "" {
		return ErrNoAddress
	}

	return nil
}

type client struct {
	clientOptions

	queuedMetrics chan Metric
}

var _ Client = &client{}

// NewClient creates a new Graphite client.
func NewClient(ctx context.Context, opts ...ClientOption) (Client, error) {
	clientOptions := clientOptions{}

	for _, opt := range opts {
		opt(&clientOptions)
	}

	if err := clientOptions.setDefaults(ctx); err != nil {
		return nil, err
	}

	return &client{
		clientOptions: clientOptions,
		queuedMetrics: make(chan Metric, clientOptions.MaxBufferSize),
	}, nil
}

// Submit submits the queued metrics to the Graphite server.
func (c *client) Submit(ctx context.Context) error {
	go func() {
		io.Copy(os.Stderr, c.Conn)
	}()

	for {
		metricStrings := make([]string, 0, c.MaxMetricsPerMessage)

		select {
		case metric := <-c.queuedMetrics:
			metricStrings = append(metricStrings, metric.String())
		case <-ctx.Done():
			return fmt.Errorf(
				"graphite: context cancelled while submitting: %w",
				ctx.Err(),
			)
		}

		furtherMetrics := true

		for i := 1; furtherMetrics && i < c.MaxMetricsPerMessage; i++ {
			select {
			case metric := <-c.queuedMetrics:
				metricStrings = append(metricStrings, metric.String())
			case <-ctx.Done():
				return fmt.Errorf(
					"graphite: context cancelled while submitting: %w",
					ctx.Err(),
				)
			default:
				furtherMetrics = false
			}
		}

		if err := c.SubmitMetricsString(
			ctx,
			strings.Join(metricStrings, "\n"),
		); err != nil {
			return fmt.Errorf("graphite: failed to submit metrics: %w", err)
		}
	}
}

// SubmitMetricsString submits the given metrics string to the Graphite server, retrying for [c.MaxTries] times.
func (c *client) SubmitMetricsString(ctx context.Context, str string) (err error) {
	// Ensure line termination.
	if !strings.HasSuffix(str, "\n") {
		str += "\n"
	}

	for i := 0; i < c.MaxTries; i++ {
		if _, err = c.Conn.Write([]byte(str)); err == nil {
			return nil
		}
	}

	return fmt.Errorf(
		"graphite: failed to send metrics after %d tries: %w",
		c.MaxTries,
		err,
	)
}

// SendMetric sends a metric to the configured metric server.
func (c *client) SendMetric(
	ctx context.Context,
	metric MetricMetadata,
	value string,
	timestamp time.Time,
) error {
	queuedMetric := Metric{
		MetricMetadata: metric,
		Value:          value,
		Timestamp:      timestamp,
	}

	select {
	case c.queuedMetrics <- queuedMetric:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
