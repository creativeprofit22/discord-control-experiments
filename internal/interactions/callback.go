package interactions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

var ErrCallbackUnavailable = errors.New("interaction callback unavailable")
var ErrCallbackRateLimited = errors.New("interaction callback rate limited")

type CallbackTelemetry interface {
	InteractionTracer
	ObserveCallbackAttempt(context.Context, string, string, time.Duration)
}

type CallbackClient struct {
	client        *http.Client
	origin        *url.URL
	applicationID string
	timeout       time.Duration
	maxBody       int64
	maxResponse   int64
	telemetry     CallbackTelemetry
}

func NewCallbackClient(client *http.Client, applicationID string, timeout time.Duration, maxBody, maxResponse int64, telemetry CallbackTelemetry) (*CallbackClient, error) {
	if client == nil || !snowflakePattern.MatchString(applicationID) || timeout <= 0 || timeout > time.Minute || maxBody < 1024 || maxBody > 1<<20 || maxResponse < 1024 || maxResponse > 1<<20 || telemetry == nil {
		return nil, errors.New("invalid callback client options")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	origin, _ := url.Parse("https://discord.com/api/v10")
	return &CallbackClient{client: &copyClient, origin: origin, applicationID: applicationID, timeout: timeout, maxBody: maxBody, maxResponse: maxResponse, telemetry: telemetry}, nil
}
func (c *CallbackClient) EditOriginal(ctx context.Context, correlationID domain.CorrelationID, token string, data CallbackData) error {
	return c.request(ctx, correlationID, http.MethodPatch, token, "messages/@original", data, true, "edit_original")
}
func (c *CallbackClient) Followup(ctx context.Context, correlationID domain.CorrelationID, token string, data CallbackData) error {
	return c.request(ctx, correlationID, http.MethodPost, token, "", data, false, "followup")
}

var callbackTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,256}$`)

func (c *CallbackClient) request(ctx context.Context, correlationID domain.CorrelationID, method, token, suffix string, data CallbackData, retry bool, operation string) (err error) {
	ctx, endSpan := c.telemetry.StartInteractionSpan(ctx, "callback", correlationID)
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		endSpan(outcome)
	}()
	if !callbackTokenPattern.MatchString(token) {
		return ErrCallbackUnavailable
	}
	body, err := json.Marshal(data)
	if err != nil || int64(len(body)) > c.maxBody {
		return ErrCallbackUnavailable
	}
	attempts := 1
	if retry {
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
		path := "/webhooks/" + url.PathEscape(c.applicationID) + "/" + url.PathEscape(token)
		if suffix != "" {
			path += "/" + suffix
		}
		request, err := http.NewRequestWithContext(requestCtx, method, c.origin.String()+path, bytes.NewReader(body))
		if err != nil {
			cancel()
			return ErrCallbackUnavailable
		}
		request.Header.Set("Content-Type", "application/json")
		started := time.Now()
		response, err := c.client.Do(request)
		if err != nil {
			c.telemetry.ObserveCallbackAttempt(ctx, operation, "transport_error", time.Since(started))
			cancel()
			if retry && attempt == 0 && ctx.Err() == nil {
				continue
			}
			return ErrCallbackUnavailable
		}
		readBytes, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, c.maxResponse+1))
		closeErr := response.Body.Close()
		c.telemetry.ObserveCallbackAttempt(ctx, operation, callbackStatusClass(response.StatusCode), time.Since(started))
		cancel()
		if readErr != nil || closeErr != nil || readBytes > c.maxResponse {
			if retry && attempt == 0 {
				continue
			}
			return ErrCallbackUnavailable
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return nil
		}
		if response.StatusCode == http.StatusTooManyRequests {
			delay, ok := validRetryAfter(response.Header.Get("Retry-After"))
			if !ok || !retry || attempt > 0 {
				return ErrCallbackRateLimited
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ErrCallbackUnavailable
			case <-timer.C:
				continue
			}
		}
		if retry && attempt == 0 && response.StatusCode >= 500 {
			continue
		}
		return ErrCallbackUnavailable
	}
	return ErrCallbackUnavailable
}
func callbackStatusClass(statusCode int) string {
	if statusCode >= 100 && statusCode <= 599 {
		return strconv.Itoa(statusCode/100) + "xx"
	}
	return "transport_error"
}
func validRetryAfter(value string) (time.Duration, bool) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 || seconds > 30 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}
