// Package telemetry configures optional, content-free OpenTelemetry observability.
package telemetry

import (
	"errors"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/net/http/httpguts"
)

type settings struct {
	enabled, traces, metrics      bool
	traceProtocol, metricProtocol string
	sampler                       sdktrace.Sampler
}

func loadSettings() (settings, error) {
	var c settings
	disabled := strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED"))
	if disabled != "" {
		value, err := strconv.ParseBool(disabled)
		if err != nil {
			return c, errors.New("OTEL_SDK_DISABLED must be a boolean")
		}
		if value {
			return c, nil
		}
		c.enabled = true
	}
	for _, suffix := range []string{"ENDPOINT", "TRACES_ENDPOINT", "METRICS_ENDPOINT"} {
		if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_"+suffix)) != "" {
			c.enabled = true
		}
	}
	if !c.enabled {
		return c, nil
	}
	var err error
	if c.traces, err = signalEnabled("TRACES"); err != nil {
		return settings{}, err
	}
	if c.metrics, err = signalEnabled("METRICS"); err != nil {
		return settings{}, err
	}
	c.enabled = c.traces || c.metrics
	if !c.enabled {
		return c, nil
	}
	for _, suffix := range []string{"ENDPOINT", "TRACES_ENDPOINT", "METRICS_ENDPOINT"} {
		key := "OTEL_EXPORTER_OTLP_" + suffix
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			parsed, parseErr := url.Parse(value)
			if parseErr != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				return settings{}, errors.New(key + " must be an HTTP(S) URL without user information, query or fragment")
			}
		}
	}
	for _, suffix := range []string{"HEADERS", "TRACES_HEADERS", "METRICS_HEADERS"} {
		key := "OTEL_EXPORTER_OTLP_" + suffix
		if !validHeaders(os.Getenv(key)) {
			return settings{}, errors.New(key + " must contain valid percent-encoded header pairs")
		}
	}
	if c.traces {
		if c.traceProtocol, err = signalProtocol("TRACES"); err != nil {
			return settings{}, err
		}
		if c.sampler, err = configuredSampler(); err != nil {
			return settings{}, err
		}
	}
	if c.metrics {
		if c.metricProtocol, err = signalProtocol("METRICS"); err != nil {
			return settings{}, err
		}
		for _, key := range []string{"OTEL_METRIC_EXPORT_INTERVAL", "OTEL_METRIC_EXPORT_TIMEOUT"} {
			if value := strings.TrimSpace(os.Getenv(key)); value != "" {
				milliseconds, parseErr := strconv.ParseInt(value, 10, 64)
				if parseErr != nil || milliseconds <= 0 || milliseconds > math.MaxInt64/int64(time.Millisecond) {
					return settings{}, errors.New(key + " must be a positive number of milliseconds")
				}
			}
		}
	}
	return c, nil
}

func signalEnabled(signal string) (bool, error) {
	key := "OTEL_" + signal + "_EXPORTER"
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "", "otlp":
		return true, nil
	case "none":
		return false, nil
	default:
		return false, errors.New(key + " supports otlp or none")
	}
}

func signalProtocol(signal string) (string, error) {
	key := "OTEL_EXPORTER_OTLP_" + signal + "_PROTOCOL"
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		key = "OTEL_EXPORTER_OTLP_PROTOCOL"
		value = strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	}
	switch value {
	case "", "http/protobuf":
		return "http/protobuf", nil
	case "grpc":
		return "grpc", nil
	default:
		return "", errors.New(key + " supports http/protobuf or grpc")
	}
}

func configuredSampler() (sdktrace.Sampler, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	var sampler sdktrace.Sampler
	switch name {
	case "", "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "traceidratio", "parentbased_traceidratio":
		ratio := 1.0
		if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
			var err error
			ratio, err = strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
				return nil, errors.New("OTEL_TRACES_SAMPLER_ARG must be between zero and one")
			}
		}
		sampler = sdktrace.TraceIDRatioBased(ratio)
		if name == "parentbased_traceidratio" {
			sampler = sdktrace.ParentBased(sampler)
		}
		return sampler, nil
	default:
		return nil, errors.New("OTEL_TRACES_SAMPLER is unsupported")
	}
}

func validHeaders(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	for _, entry := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			return false
		}
		decodedKey, keyErr := url.PathUnescape(strings.TrimSpace(key))
		decodedValue, valueErr := url.PathUnescape(strings.TrimSpace(value))
		if keyErr != nil || valueErr != nil || !httpguts.ValidHeaderFieldName(decodedKey) || !httpguts.ValidHeaderFieldValue(decodedValue) {
			return false
		}
	}
	return true
}
