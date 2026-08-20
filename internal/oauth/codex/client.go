package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/crush/internal/log"
	"github.com/klauspost/compress/zstd"
)

// NewClient creates an HTTP client for Codex inference requests. The
// transport translates ChatGPT subscription errors (usage limits, plan
// gating) into descriptive messages and zstd-compresses request bodies,
// matching what the official Codex client sends to the responses endpoint.
func NewClient(debug bool) *http.Client {
	base := baseTransport(debug)
	errorBase := &errorTransport{base: base}
	return &http.Client{Transport: &zstdTransport{base: errorBase}}
}

func baseTransport(debug bool) http.RoundTripper {
	if debug {
		return log.NewHTTPClient().Transport
	}
	return http.DefaultTransport
}

// zstdTransport compresses JSON request bodies bound for the Codex
// responses endpoint. The ChatGPT backend accepts Content-Encoding: zstd
// on the SSE responses endpoint (the official Codex client does the same).
type zstdTransport struct {
	base http.RoundTripper
}

func (t *zstdTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Method == http.MethodPost &&
		strings.Contains(req.URL.Path, "/responses") &&
		req.Header.Get("Content-Encoding") == "" &&
		strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err == nil {
			if compressed, cerr := compressZstd(body); cerr == nil {
				req.Body = io.NopCloser(bytes.NewReader(compressed))
				req.ContentLength = int64(len(compressed))
				req.Header.Set("Content-Encoding", "zstd")
			} else {
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
	}
	return t.base.RoundTrip(req)
}

// compressZstd compresses b with a Codex-compatible zstd level.
func compressZstd(b []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(b, make([]byte, 0, len(b)/2)), nil
}

type errorTransport struct {
	base http.RoundTripper
}

func (t *errorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Leave success, auth, and streaming responses untouched. Auth errors
	// must reach the refresh machinery verbatim.
	if resp.StatusCode < http.StatusBadRequest ||
		resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, err
	}

	translated, ok := translateError(body)
	if ok {
		resp.Body = io.NopCloser(strings.NewReader(translated))
		resp.Header.Set("Content-Length", fmt.Sprint(len(translated)))
	} else {
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// translateError decodes a Codex backend error body and, when it
// describes a known subscription condition, rewrites the JSON error
// message with user-facing detail. It returns ok=false when the body is
// left untouched.
func translateError(body []byte) (string, bool) {
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		PlanType string `json:"plan_type"`
		ResetsAt string `json:"resets_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false
	}

	message := translateErrorMessage(parsed.Error.Code, parsed.Error.Message, parsed.PlanType, parsed.ResetsAt)
	if message == "" {
		return "", false
	}

	// Keep the original shape so downstream error decoding still works,
	// only the message text is upgraded.
	if parsed.Error.Code != "" {
		parsed.Error.Message = message
		rewritten, err := json.Marshal(parsed)
		if err == nil {
			return string(rewritten), true
		}
	}

	// Fall back to a plain JSON error object when the original shape
	// cannot be preserved.
	fallback := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	}
	rewritten, err := json.Marshal(fallback)
	if err != nil {
		return "", false
	}
	return string(rewritten), true
}

func translateErrorMessage(code, original, planType, resetsAt string) string {
	switch code {
	case "usage_limit_reached", "usage_not_included", "usage_limit_exceeded",
		"rate_limit_exceeded", "insufficient_quota", "free_usage_limit_reached",
		"goplus_usage_limit", "model_not_included_in_plan":
		plan := planType
		if plan == "" {
			plan = "current"
		}
		message := fmt.Sprintf("You have hit your ChatGPT usage limit (%s plan).", plan)
		if resetsAt != "" {
			message += fmt.Sprintf(" Try again at %s.", resetsAt)
		} else if original != "" {
			message += " " + original
		}
		return message
	case "account_not_found", "invalid_api_key":
		message := "Your ChatGPT login has expired. Run 'crush login codex' to re-authenticate."
		if original != "" {
			message += " " + original
		}
		return message
	default:
		return ""
	}
}
