package telemetry

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

const dsn = "https://bdb39394e239547033d75e6275ff10ae@o188824.ingest.us.sentry.io/4512351899025408"

// Init creates an isolated SDK client. Defer shutdown to flush buffered
// telemetry before main exits, including when a command panics.
func Init(ctx context.Context) (context.Context, func()) {
	if strings.EqualFold(os.Getenv("TELEMETRY_DISABLED"), "true") {
		return ctx, func() {}
	}
	client, err := sentry.NewClient(options())
	if err != nil {
		// Observability must not prevent a command from running.
		return ctx, func() {}
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	return sentry.SetHubOnContext(ctx, hub), func() {
		client.Flush(2 * time.Second)
		client.Close()
	}
}

// RunCommand records only the static command path, never arguments or output.
func RunCommand(ctx context.Context, name string, execute func(context.Context) error) (err error) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil || hub.Client() == nil {
		return execute(ctx)
	}
	hub.Scope().SetTag("cli.command", name)
	span := sentry.StartTransaction(ctx, name, sentry.WithOpName("console.command"))
	ctx = span.Context()
	hub.AddBreadcrumb(&sentry.Breadcrumb{Category: "cli.command", Message: "CLI command invoked", Level: sentry.LevelInfo}, nil)
	logger := sentry.NewLogger(ctx)
	logger.SetAttributes(attribute.String("cli.command", name))
	meter := sentry.NewMeter(ctx)
	meter.SetAttributes(attribute.String("cli.command", name))
	logger.Info().Emit("CLI command started")
	started := time.Now()
	defer func() {
		panicValue := recover()
		status := "ok"
		span.Status = sentry.SpanStatusOK
		if err != nil || panicValue != nil {
			status = "error"
			span.Status = sentry.SpanStatusInternalError
			logger.Error().Emit("CLI command failed")
			if panicValue != nil {
				hub.RecoverWithContext(ctx, panicValue)
			} else {
				hub.CaptureException(err)
			}
		} else {
			logger.Info().Emit("CLI command completed")
		}
		attrs := sentry.WithAttributes(attribute.String("cli.status", status))
		meter.Count("gh_kit.command.count", 1, attrs)
		meter.Distribution("gh_kit.command.duration", time.Since(started).Seconds(), attrs, sentry.WithUnit(sentry.UnitSecond))
		meter.Gauge("gh_kit.runtime.goroutines", float64(runtime.NumGoroutine()))
		span.Finish()
		if panicValue != nil {
			panic(panicValue)
		}
	}()
	return execute(ctx)
}

// TraceRequest times a GitHub API call without collecting its URL or payload
// or propagating trace headers to the external service.
func TraceRequest(ctx context.Context, method string, request func(context.Context) error) error {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil || sentry.SpanFromContext(ctx) == nil {
		return request(ctx)
	}
	span := sentry.StartSpan(ctx, "http.client", sentry.WithDescription(method+" GitHub API request"))
	defer span.Finish()
	span.SetData("http.request.method", method)
	err := request(span.Context())
	span.Status = sentry.SpanStatusOK
	level := sentry.LevelInfo
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		level = sentry.LevelError
	}
	hub.AddBreadcrumb(&sentry.Breadcrumb{Category: "http", Message: "GitHub API request", Level: level, Data: map[string]interface{}{"http.request.method": method}}, nil)
	return err
}

func options() sentry.ClientOptions {
	return sentry.ClientOptions{
		Dsn:                   dsn,
		Release:               release(),
		Environment:           "production",
		EnableTracing:         true,
		TracesSampleRate:      1,
		AttachStacktrace:      true,
		SendDefaultPII:        false,
		MaxBreadcrumbs:        10,
		BeforeBreadcrumb:      sanitizeBreadcrumb,
		BeforeSend:            sanitizeEvent,
		BeforeSendTransaction: sanitizeEvent,
		BeforeSendLog: func(log *sentry.Log) *sentry.Log {
			switch log.Body {
			case "CLI command started", "CLI command completed", "CLI command failed":
				log.Attributes = safeAttributes(log.Attributes)
				return log
			default:
				return nil
			}
		},
		BeforeSendMetric: func(metric *sentry.Metric) *sentry.Metric {
			switch metric.Name {
			case "gh_kit.command.count", "gh_kit.command.duration", "gh_kit.runtime.goroutines":
				metric.Attributes = safeAttributes(metric.Attributes)
				return metric
			default:
				return nil
			}
		},
	}
}

func release() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return "gh-kit@" + info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return "gh-kit@" + setting.Value
			}
		}
	}
	return "gh-kit@development"
}

func safeAttributes(attrs map[string]attribute.Value) map[string]attribute.Value {
	for key := range attrs {
		switch key {
		case "cli.command", "cli.status", "sentry.release", "sentry.environment", "sentry.sdk.name", "sentry.sdk.version":
		default:
			delete(attrs, key)
		}
	}
	return attrs
}

func sanitizeEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	clean := &sentry.Event{
		EventID:         event.EventID,
		Timestamp:       event.Timestamp,
		Level:           event.Level,
		Platform:        event.Platform,
		Release:         event.Release,
		Environment:     event.Environment,
		Sdk:             event.Sdk,
		Modules:         event.Modules,
		Type:            event.Type,
		Transaction:     event.Transaction,
		StartTime:       event.StartTime,
		TransactionInfo: event.TransactionInfo,
		Contexts:        map[string]sentry.Context{},
	}
	for _, key := range []string{"trace", "os", "runtime", "device"} {
		if value, ok := event.Contexts[key]; ok {
			clean.Contexts[key] = value
		}
	}
	if event.Message != "" {
		clean.Message = "CLI command failed"
	}
	for _, breadcrumb := range event.Breadcrumbs {
		if safe := sanitizeBreadcrumb(breadcrumb, nil); safe != nil {
			clean.Breadcrumbs = append(clean.Breadcrumbs, safe)
		}
	}
	for _, span := range event.Spans {
		if span.Op != "http.client" {
			continue
		}
		method, _ := span.Data["http.request.method"].(string)
		clean.Spans = append(clean.Spans, &sentry.Span{
			TraceID: span.TraceID, SpanID: span.SpanID, ParentSpanID: span.ParentSpanID,
			Op: span.Op, Description: method + " GitHub API request", Status: span.Status,
			StartTime: span.StartTime, EndTime: span.EndTime,
			Data: map[string]interface{}{"http.request.method": method},
		})
	}
	if name := event.Tags["cli.command"]; name != "" {
		clean.Tags = map[string]string{"cli.command": name}
	}
	for _, exception := range event.Exception {
		clean.Exception = append(clean.Exception, sentry.Exception{
			Type:       exception.Type,
			Value:      "CLI command failed",
			Stacktrace: sanitizeStacktrace(exception.Stacktrace),
		})
	}
	for _, thread := range event.Threads {
		clean.Threads = append(clean.Threads, sentry.Thread{
			ID:         thread.ID,
			Crashed:    thread.Crashed,
			Current:    thread.Current,
			Stacktrace: sanitizeStacktrace(thread.Stacktrace),
		})
	}
	return clean
}

func sanitizeBreadcrumb(breadcrumb *sentry.Breadcrumb, _ *sentry.BreadcrumbHint) *sentry.Breadcrumb {
	clean := &sentry.Breadcrumb{Category: breadcrumb.Category, Level: breadcrumb.Level, Timestamp: breadcrumb.Timestamp}
	switch breadcrumb.Category {
	case "cli.command":
		clean.Message = "CLI command invoked"
	case "http":
		clean.Message = "GitHub API request"
		clean.Data = map[string]interface{}{"http.request.method": breadcrumb.Data["http.request.method"]}
	default:
		return nil
	}
	return clean
}

func sanitizeStacktrace(stack *sentry.Stacktrace) *sentry.Stacktrace {
	if stack == nil {
		return nil
	}
	clean := &sentry.Stacktrace{}
	for _, frame := range stack.Frames {
		filename := frame.Filename
		if filename == "" {
			filename = frame.AbsPath
		}
		if filename != "" {
			filename = filepath.Base(filename)
			if pkg, ok := strings.CutPrefix(frame.Module, "github.com/philprime/gh-kit/"); ok {
				filename = path.Join(pkg, filename)
			}
		}
		clean.Frames = append(clean.Frames, sentry.Frame{
			Function: frame.Function,
			Module:   frame.Module,
			Filename: filename,
			Lineno:   frame.Lineno,
			InApp:    frame.InApp,
		})
	}
	return clean
}
