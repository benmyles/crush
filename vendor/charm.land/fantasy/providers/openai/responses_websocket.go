// WebSocket transport for the OpenAI Responses API. Used by backends that
// serve the same event protocol over a WebSocket (notably the ChatGPT Codex
// subscription endpoint), where each text frame carries one Responses
// stream event and requests are sent as a {"type":"response.create", ...}
// frame mirroring the SSE request body.
package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/gorilla/websocket"
	"github.com/openai/openai-go/v3/responses"
)

const (
	// responsesWebSocketsBeta is the OpenAI-Beta header value the Codex
	// backend requires for WebSocket response streams. The SSE value
	// (responses=experimental) is rejected on the WebSocket handshake.
	responsesWebSocketsBeta = "responses_websockets=2026-02-06"

	websocketConnectTimeout = 15 * time.Second
)

// responsesEventStream is the iteration contract shared by the SSE event
// stream and the WebSocket event stream.
type responsesEventStream interface {
	Next() bool
	Current() responses.ResponseStreamEventUnion
	Err() error
}

// responsesWebSocketStream adapts a Codex-style WebSocket response stream
// to the same event surface as the SSE stream.
type responsesWebSocketStream struct {
	conn *websocket.Conn

	current     responses.ResponseStreamEventUnion
	err         error
	buffered    bool
	terminal    bool
	notifiedErr bool
}

// newResponsesWebSocketStream dials the WebSocket responses endpoint and
// sends the response.create frame. The returned stream must be advanced
// with Next before Current is meaningful.
func newResponsesWebSocketStream(ctx context.Context, transportOpts responsesTransportOptions, callHeaders map[string]string, params responses.ResponseNewParams) (responsesEventStream, error) {
	wsURL := transportOpts.websocketURL
	if wsURL == "" {
		wsURL = websocketURLFromBase(transportOpts.baseURL)
	}

	header := make(http.Header)
	for k, v := range transportOpts.requestHeaders {
		header.Set(k, v)
	}
	for k, v := range transportOpts.websocketHeaders {
		header.Set(k, v)
	}
	for k, v := range callHeaders {
		header.Set(k, v)
	}
	// The WebSocket handshake is a different protocol than the SSE request:
	// drop body-related headers and swap the beta header.
	header.Del("Accept")
	header.Del("Content-Type")
	header.Del("Openai-Beta")
	header.Del("openai-beta")
	header.Set("OpenAI-Beta", responsesWebSocketsBeta)
	if transportOpts.apiKey != "" {
		header.Set("Authorization", "Bearer "+transportOpts.apiKey)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: websocketConnectTimeout,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, err
	}

	body, err := responseCreateFrame(params)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
		conn.Close()
		return nil, err
	}

	return &responsesWebSocketStream{conn: conn}, nil
}

// websocketURLFromBase derives the WebSocket endpoint from the Responses
// base URL. The SSE endpoint is <base>/responses; the WebSocket endpoint is
// the same URL with the scheme swapped.
func websocketURLFromBase(baseURL string) string {
	raw := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(raw, "/responses") {
		raw += "/responses"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	return u.String()
}

// responseCreateFrame builds the {"type":"response.create", ...} frame
// from the request parameters, mirroring the SSE request body.
func responseCreateFrame(params responses.ResponseNewParams) ([]byte, error) {
	raw, err := params.MarshalJSON()
	if err != nil {
		return nil, err
	}
	frame := map[string]any{"type": "response.create"}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}
	return json.Marshal(frame)
}

// Next advances to the next event. The first Next after creation replays
// the same event consumed during the preflight read.
func (s *responsesWebSocketStream) Next() bool {
	if s.err != nil {
		return false
	}
	if s.terminal || s.notifiedErr {
		return false
	}
	if s.buffered {
		s.buffered = false
		return true
	}
	s.current = responses.ResponseStreamEventUnion{}
	_, message, err := s.conn.ReadMessage()
	if err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			s.err = io.EOF
		} else if websocket.IsUnexpectedCloseError(err) && !s.terminal {
			s.err = err
		} else {
			s.err = io.EOF
		}
		return false
	}
	if err := json.Unmarshal(message, &s.current); err != nil {
		s.err = err
		return false
	}
	// Some backend revisions signal completion with "response.done" and an
	// embedded response payload identical to response.completed. Normalize
	// it so the shared event handling sees one terminal shape.
	if s.current.Type == "response.done" {
		s.current.Type = "response.completed"
	}
	switch s.current.Type {
	case "response.completed", "response.incomplete", "response.failed":
		s.terminal = true
	case "error":
		s.notifiedErr = true
	}
	return true
}

// Current returns the event produced by the last successful Next.
func (s *responsesWebSocketStream) Current() responses.ResponseStreamEventUnion {
	return s.current
}

// Err returns the terminal stream error, io.EOF on a clean end.
func (s *responsesWebSocketStream) Err() error {
	return s.err
}

// openResponsesStream picks the WebSocket transport when enabled, falling
// back to the SSE stream when the connection cannot be established or dies
// before its first event. API-level failures (an "error" frame) count as
// events and are not retried over SSE.
func (o responsesLanguageModel) openResponsesStream(ctx context.Context, call fantasy.Call, params responses.ResponseNewParams) responsesEventStream {
	sseStream := func() responsesEventStream {
		return o.client.Responses.NewStreaming(ctx, params, append(callUARequestOptions(call), callHeadersRequestOptions(call)...)...)
	}

	if !o.transportOpts.websocketEnabled {
		return sseStream()
	}

	wsStream, err := newResponsesWebSocketStream(ctx, o.transportOpts, call.Headers, params)
	if err != nil {
		return sseStream()
	}
	typed, ok := wsStream.(*responsesWebSocketStream)
	if !ok {
		return sseStream()
	}
	// Preflight: the first frame must arrive on the WebSocket, otherwise
	// the connection is unhealthy and SSE is used instead.
	if !typed.Next() {
		return sseStream()
	}
	typed.buffered = true
	return typed
}
