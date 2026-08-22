package fireworksdsv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
)

var errStopStream = errors.New("stop Fireworks DSV4 stream")

func completionEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing Fireworks DSV4 base URL: %w", err)
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	parsed.Path = path + "/completions"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func responseHeaderMap(header http.Header) map[string]string {
	result := make(map[string]string, len(header))
	for name, values := range header {
		result[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	return result
}

func requestedRetryDelay(header http.Header) (time.Duration, bool) {
	if value := header.Get("retry-after-ms"); value != "" {
		milliseconds, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return time.Duration(milliseconds * float64(time.Millisecond)), true
		}
	}
	if value := header.Get("retry-after"); value != "" {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil {
			return time.Duration(seconds * float64(time.Second)), true
		}
		if date, err := http.ParseTime(value); err == nil {
			return time.Until(date), true
		}
	}
	return 0, false
}

func (l *languageModel) postCompletion(ctx context.Context, call fantasy.Call, payload map[string]any) (*http.Response, error) {
	endpoint, err := completionEndpoint(l.baseURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling Fireworks DSV4 request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating Fireworks DSV4 request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	if l.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+l.apiKey)
	}
	for name, value := range l.headers {
		request.Header.Set(name, value)
	}
	for name, value := range call.Headers {
		if value == "" {
			request.Header.Del(name)
		} else {
			request.Header.Set(name, value)
		}
	}
	if call.UserAgent != "" {
		request.Header.Set("User-Agent", call.UserAgent)
	}
	response, err := l.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	defer response.Body.Close()
	errorBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
	if readErr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(errorBody) > maxErrorBodyBytes {
		errorBody = errorBody[:maxErrorBodyBytes]
	}
	message := strings.TrimSpace(string(errorBody))
	if message == "" {
		message = response.Status
	}
	retryableStatus := response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusConflict ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError
	serverRequestsRetry := strings.EqualFold(response.Header.Get("x-should-retry"), "true")
	if delay, ok := requestedRetryDelay(response.Header); ok && (retryableStatus || serverRequestsRetry) && delay > time.Minute {
		return nil, &fantasy.Error{
			Title:   "Fireworks retry delay too long",
			Message: fmt.Sprintf("Server requested a %s retry delay, above the one-minute limit", delay.Round(time.Second)),
		}
	}
	if strings.EqualFold(response.Header.Get("x-should-retry"), "false") && retryableStatus {
		return nil, &fantasy.Error{Title: "Fireworks request failed", Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, message)}
	}
	return nil, &fantasy.ProviderError{
		Title:           "Fireworks request failed",
		Message:         message,
		URL:             endpoint,
		StatusCode:      response.StatusCode,
		RequestBody:     body,
		ResponseHeaders: responseHeaderMap(response.Header),
		ResponseBody:    errorBody,
		TransientError:  response.StatusCode == http.StatusTooEarly,
	}
}

type sseDecoder struct {
	buffer    []byte
	dataLines []string
	done      bool
}

func (d *sseDecoder) event() (map[string]any, bool, error) {
	if len(d.dataLines) == 0 {
		return nil, false, nil
	}
	data := strings.Join(d.dataLines, "\n")
	d.dataLines = nil
	if strings.HasPrefix(data, "[DONE]") {
		d.done = true
		return nil, true, nil
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("parsing Fireworks SSE event: %w", err)
	}
	return payload, true, nil
}

func (d *sseDecoder) line(value string) (map[string]any, bool, error) {
	if value == "" {
		return d.event()
	}
	if strings.HasPrefix(value, "data:") {
		d.dataLines = append(d.dataLines, strings.TrimLeft(value[5:], " \t"))
	}
	return nil, false, nil
}

func nextLine(buffer []byte, final bool) (line []byte, consumed int, ok bool) {
	for index, value := range buffer {
		if value != '\r' && value != '\n' {
			continue
		}
		if value == '\r' && index+1 == len(buffer) && !final {
			return nil, 0, false
		}
		consumed = index + 1
		if value == '\r' && index+1 < len(buffer) && buffer[index+1] == '\n' {
			consumed++
		}
		return buffer[:index], consumed, true
	}
	if final && len(buffer) > 0 {
		return buffer, len(buffer), true
	}
	return nil, 0, false
}

func consumeSSE(ctx context.Context, body io.Reader, consume func(map[string]any) error) error {
	decoder := &sseDecoder{}
	chunk := make([]byte, 32*1024)
	for {
		count, readErr := body.Read(chunk)
		if count > 0 {
			decoder.buffer = append(decoder.buffer, chunk[:count]...)
		}
		final := readErr != nil
		for {
			line, consumed, ok := nextLine(decoder.buffer, final)
			if !ok {
				break
			}
			decoder.buffer = decoder.buffer[consumed:]
			payload, present, err := decoder.line(string(line))
			if err != nil {
				return err
			}
			if decoder.done {
				return nil
			}
			if present && payload != nil {
				if err := consume(payload); err != nil {
					return err
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			if payload, present, err := decoder.event(); err != nil {
				return err
			} else if present && payload != nil {
				if err := consume(payload); err != nil {
					return err
				}
			}
			if !decoder.done {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
	}
}

func responseMediaType(response *http.Response) string {
	value := response.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

func consumePayloads(ctx context.Context, response *http.Response, consume func(map[string]any) error) error {
	defer response.Body.Close()
	if responseMediaType(response) == "text/event-stream" {
		return consumeSSE(ctx, response.Body, consume)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("parsing Fireworks JSON response: %w", err)
	}
	return consume(payload)
}
