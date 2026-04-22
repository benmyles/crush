package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
)

const morphCompactModel = "morph-compactor"

var (
	ErrMorphCompactDisabled      = errors.New("Morph Compact is not enabled")
	ErrMorphCompactAPIKeyMissing = errors.New("Morph Compact API key is missing")
)

type morphCompactMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type morphCompactRequest struct {
	Messages          []morphCompactMessage `json:"messages,omitempty"`
	Query             string                `json:"query,omitempty"`
	CompressionRatio  float64               `json:"compression_ratio,omitempty"`
	PreserveRecent    int                   `json:"preserve_recent"`
	IncludeMarkers    bool                  `json:"include_markers"`
	IncludeLineRanges bool                  `json:"include_line_ranges"`
	Model             string                `json:"model,omitempty"`
}

type morphCompactUsage struct {
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CompressionRatio float64 `json:"compression_ratio"`
	ProcessingTimeMS int64   `json:"processing_time_ms"`
}

type morphCompactResponse struct {
	ID     string            `json:"id"`
	Model  string            `json:"model"`
	Output string            `json:"output"`
	Usage  morphCompactUsage `json:"usage"`
}

type morphCompactClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func newMorphCompactClient(opts config.MorphCompactOptions) (*morphCompactClient, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	if apiKey == "" {
		return nil, ErrMorphCompactAPIKeyMissing
	}
	return &morphCompactClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(strings.TrimSpace(opts.EffectiveBaseURL()), "/"),
		client:  &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

func (c *morphCompactClient) compact(ctx context.Context, req morphCompactRequest) (*morphCompactResponse, error) {
	endpoint, err := url.JoinPath(c.baseURL, "compact")
	if err != nil {
		return nil, fmt.Errorf("invalid Morph Compact base_url: %w", err)
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to encode Morph Compact request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to build Morph Compact request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Morph Compact request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Morph Compact request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var compactResp morphCompactResponse
	if err := json.NewDecoder(resp.Body).Decode(&compactResp); err != nil {
		return nil, fmt.Errorf("failed to decode Morph Compact response: %w", err)
	}
	if strings.TrimSpace(compactResp.Output) == "" {
		return nil, errors.New("Morph Compact returned empty output")
	}
	return &compactResp, nil
}

func (a *sessionAgent) Compact(ctx context.Context, sessionID string, opts config.MorphCompactOptions, providerOpts fantasy.ProviderOptions) error {
	return a.compact(ctx, sessionID, opts, providerOpts, false)
}

func (a *sessionAgent) SummarizeThenCompact(ctx context.Context, sessionID string, opts config.MorphCompactOptions, providerOpts fantasy.ProviderOptions) error {
	return a.compact(ctx, sessionID, opts, providerOpts, true)
}

func (a *sessionAgent) compact(ctx context.Context, sessionID string, opts config.MorphCompactOptions, providerOpts fantasy.ProviderOptions, summarizeFirst bool) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}
	if !opts.Enabled {
		return ErrMorphCompactDisabled
	}
	if err := validateMorphCompactOptions(opts); err != nil {
		return err
	}

	client, err := newMorphCompactClient(opts)
	if err != nil {
		return err
	}

	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	compactMessages := buildMorphCompactMessages(msgs, currentSession.Todos)
	if len(compactMessages) == 0 {
		return nil
	}

	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(sessionID, cancel)
	defer a.activeRequests.Del(sessionID)
	defer cancel()

	a.publishCompactionNotification(pubsub.CreatedEvent, notify.TypeCompactionStarted, currentSession)
	defer a.publishCompactionNotification(pubsub.CreatedEvent, notify.TypeCompactionFinished, currentSession)

	var generated generatedSummary
	if summarizeFirst {
		largeModel := a.largeModel.Get()
		systemPromptPrefix := a.systemPromptPrefix.Get()
		summaryMessage := message.Message{
			Role:             message.Assistant,
			SessionID:        sessionID,
			Model:            largeModel.Model.Model(),
			Provider:         largeModel.Model.Provider(),
			IsSummaryMessage: true,
		}
		var err error
		generated, err = a.generateSummary(genCtx, currentSession, msgs, providerOpts, largeModel, systemPromptPrefix, summaryMessage, nil)
		if err != nil {
			return err
		}
	}

	resp, err := client.compact(genCtx, morphCompactRequest{
		Messages:          compactMessages,
		Query:             latestUserQuery(msgs, currentSession.Title),
		CompressionRatio:  opts.EffectiveCompressionRatio(),
		PreserveRecent:    opts.EffectivePreserveRecent(),
		IncludeMarkers:    true,
		IncludeLineRanges: false,
		Model:             morphCompactModel,
	})
	if err != nil {
		return err
	}

	model := resp.Model
	if model == "" {
		model = morphCompactModel
	}
	strategy := config.PlanCompactStrategyMorph
	if summarizeFirst {
		strategy = config.PlanCompactStrategySummarizeThenMorph
	}
	compactMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Parts:            []message.ContentPart{message.TextContent{Text: compactedContextContent(strategy, compactedContextSourceMorph, resp.Output)}},
		Provider:         "morph",
		Model:            model,
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}
	compactMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(genCtx, compactMessage); err != nil {
		return err
	}

	var summaryTokens int64
	if summarizeFirst {
		generatedMessage := generated.message.Clone()
		setMessageTextContent(
			&generatedMessage,
			compactedContextContent(config.PlanCompactStrategySummarizeThenMorph, compactedContextSourceModelSummary, generatedMessage.Content().Text),
		)
		_, err = a.messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:             generatedMessage.Role,
			Parts:            generatedMessage.Parts,
			Provider:         generatedMessage.Provider,
			Model:            generatedMessage.Model,
			IsSummaryMessage: true,
		})
		if err != nil {
			return err
		}

		a.updateSessionUsage(generated.model, &currentSession, generated.totalUsage, generated.openrouterCost)

		summaryTokens = generated.responseUsage.OutputTokens
		if summaryTokens == 0 {
			summaryTokens = approximateTokenCount(generated.message.Content().Text)
		}
	}
	compactTokens := resp.Usage.OutputTokens
	if compactTokens == 0 {
		compactTokens = approximateTokenCount(resp.Output)
	}

	currentSession.SummaryMessageID = compactMessage.ID
	currentSession.PromptTokens = 0
	currentSession.CompletionTokens = compactTokens + summaryTokens
	_, err = a.sessions.Save(genCtx, currentSession)
	return err
}

func (a *sessionAgent) publishCompactionNotification(eventType pubsub.EventType, notificationType notify.Type, currentSession session.Session) {
	if a.notify == nil {
		return
	}
	a.notify.Publish(eventType, notify.Notification{
		SessionID:    currentSession.ID,
		SessionTitle: currentSession.Title,
		Type:         notificationType,
	})
}

func validateMorphCompactOptions(opts config.MorphCompactOptions) error {
	ratio := opts.EffectiveCompressionRatio()
	if ratio < 0.05 || ratio > 1 {
		return errors.New("Morph Compact compression_ratio must be between 0.05 and 1")
	}
	if opts.EffectivePreserveRecent() < 0 {
		return errors.New("Morph Compact preserve_recent must be greater than or equal to 0")
	}
	return nil
}

func buildMorphCompactMessages(msgs []message.Message, todos []session.Todo) []morphCompactMessage {
	compactMessages := make([]morphCompactMessage, 0, len(msgs)+1)
	for _, msg := range msgs {
		content := compactMessageContent(msg)
		if strings.TrimSpace(content) == "" {
			continue
		}
		compactMessages = append(compactMessages, morphCompactMessage{
			Role:    string(msg.Role),
			Content: content,
		})
	}
	if todoContent := morphTodoContent(todos); todoContent != "" {
		compactMessages = append(compactMessages, morphCompactMessage{
			Role:    string(message.System),
			Content: todoContent,
		})
	}
	return compactMessages
}

func compactMessageContent(msg message.Message) string {
	switch msg.Role {
	case message.User:
		return compactUserMessageContent(msg)
	case message.Assistant:
		return compactAssistantMessageContent(msg)
	case message.Tool:
		return compactToolMessageContent(msg)
	default:
		return strings.TrimSpace(msg.Content().Text)
	}
}

func compactUserMessageContent(msg message.Message) string {
	text := strings.TrimSpace(msg.Content().Text)
	var textAttachments []message.Attachment
	for _, content := range msg.BinaryContent() {
		if !strings.HasPrefix(content.MIMEType, "text/") {
			continue
		}
		textAttachments = append(textAttachments, message.Attachment{
			FilePath: content.Path,
			MimeType: content.MIMEType,
			Content:  content.Data,
		})
	}
	return strings.TrimSpace(message.PromptWithTextAttachments(text, textAttachments))
}

func compactAssistantMessageContent(msg message.Message) string {
	var sb strings.Builder
	if text := strings.TrimSpace(msg.Content().Text); text != "" {
		sb.WriteString(text)
	}
	for _, call := range msg.ToolCalls() {
		writeSectionSpacing(&sb)
		fmt.Fprintf(&sb, "<tool_call id=%q name=%q>\n", call.ID, call.Name)
		sb.WriteString(strings.TrimSpace(call.Input))
		sb.WriteString("\n</tool_call>")
	}
	return strings.TrimSpace(sb.String())
}

func compactToolMessageContent(msg message.Message) string {
	var sb strings.Builder
	for _, result := range msg.ToolResults() {
		writeSectionSpacing(&sb)
		fmt.Fprintf(&sb, "<tool_result id=%q name=%q is_error=\"%t\">\n", result.ToolCallID, result.Name, result.IsError)
		switch {
		case result.Content != "":
			sb.WriteString(strings.TrimSpace(result.Content))
		case result.Data != "":
			fmt.Fprintf(&sb, "[media tool result omitted: %s]", result.MIMEType)
		}
		sb.WriteString("\n</tool_result>")
	}
	return strings.TrimSpace(sb.String())
}

func morphTodoContent(todos []session.Todo) string {
	if len(todos) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<keepContext>\n")
	sb.WriteString("## Current Todo List\n\n")
	for _, todo := range todos {
		fmt.Fprintf(&sb, "- [%s] %s\n", todo.Status, todo.Content)
	}
	sb.WriteString("\nUse the `todos` tool to continue tracking progress on these tasks.\n")
	sb.WriteString("</keepContext>")
	return sb.String()
}

func latestUserQuery(msgs []message.Message, fallback string) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != message.User {
			continue
		}
		text := strings.TrimSpace(msgs[i].Content().Text)
		if text != "" {
			return text
		}
	}
	return fallback
}

func writeSectionSpacing(sb *strings.Builder) {
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
}
