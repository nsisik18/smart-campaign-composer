package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

type AnthropicConfig struct {
	APIKey  string
	Model   string
	Timeout time.Duration
	Effort  string // low | medium | high
	// BaseURL yalnızca testte httptest sunucusuna yönlendirmek için.
	BaseURL string
}

// Anthropic gerçek Claude istemcisidir. API key yalnızca backend'de durur.
type Anthropic struct {
	client anthropic.Client
	cfg    AnthropicConfig
}

func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		// 429/529/5xx ve bağlantı hatasında SDK tek retry yapar (§4.2), sonra hata.
		option.WithMaxRetries(1),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Effort == "" {
		cfg.Effort = "low"
	}
	return &Anthropic{client: anthropic.NewClient(opts...), cfg: cfg}
}

func (a *Anthropic) Segment(ctx context.Context, req SegmentRequest) (Result, error) {
	// Müşteri PII'ı gönderilmez: yalnızca brief ve (onarımda) kendi önceki çıktısı.
	msgs := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("<brief>\n" + req.Brief + "\n</brief>")),
	}
	if req.Repair != nil {
		msgs = append(msgs,
			anthropic.NewAssistantMessage(anthropic.NewTextBlock(req.Repair.PreviousOutput)),
			anthropic.NewUserMessage(anthropic.NewTextBlock(repairInstruction(req.Repair.Issues))),
		)
	}

	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: segmentPromptV1}},
		Messages:  msgs,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(a.cfg.Effort),
			Format: anthropic.JSONOutputFormatParam{Schema: SegmentSchema()},
		},
	})
	res := Result{Model: a.cfg.Model, PromptVersion: SegmentPromptVersion}
	if err != nil {
		return res, classify(ctx, err)
	}
	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens:
		return res, ErrTruncated
	case anthropic.StopReasonRefusal:
		return res, fmt.Errorf("%w: %s", ErrRefusal, msg.StopDetails.Category)
	}
	var sb strings.Builder
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	res.Text = sb.String()
	res.Model = string(msg.Model)
	return res, nil
}

func classify(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%w: status %d", ErrUpstream, apiErr.StatusCode)
	}
	return fmt.Errorf("%w: %v", ErrUpstream, err)
}

// repairInstruction hata listesini makine dilinde iletir; model önceki
// çıktısını düzeltip yalnızca yeni JSON'u döner.
func repairInstruction(issues []string) string {
	var b strings.Builder
	b.WriteString("Önceki çıktın backend doğrulamasından geçmedi. Hatalar:\n")
	for _, i := range issues {
		b.WriteString("- ")
		b.WriteString(i)
		b.WriteString("\n")
	}
	b.WriteString("Aynı brief için kurallara uyan düzeltilmiş çıktıyı üret. İfade edemediğin kriteri unsupported_criteria'ya taşı; alan uydurma.")
	return b.String()
}
