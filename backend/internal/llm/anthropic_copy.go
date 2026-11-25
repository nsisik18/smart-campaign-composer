package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

func copyUserMessage(req CopyRequest) string {
	msg := fmt.Sprintf("<brief>\n%s\n</brief>\n\nHedef kitle: %s\n\nRender sınırı: kitledeki en uzun {{first_name}} değeri %d karakter, en uzun {{city}} değeri %d karakter.",
		req.Brief, req.SegmentSummary, req.MaxFirstNameLen, req.MaxCityLen)
	if len(req.UnsupportedCriteria) > 0 {
		msg += "\n\nUygulanamayan kriterler (kitleye UYGULANMADI, metin bunlara özelmiş gibi yazılmamalı): " + strings.Join(req.UnsupportedCriteria, "; ")
	}
	return msg
}

func (a *Anthropic) StreamCopy(ctx context.Context, req CopyRequest, onText func(string)) (Result, error) {
	msgs := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(copyUserMessage(req)))}
	if req.Repair != nil {
		msgs = append(msgs,
			anthropic.NewAssistantMessage(anthropic.NewTextBlock(req.Repair.PreviousOutput)),
			anthropic.NewUserMessage(anthropic.NewTextBlock(repairInstruction(req.Repair.Issues))),
		)
	}
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	stream := a.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: copyPrompt}},
		Messages:  msgs,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(a.cfg.Effort),
			Format: anthropic.JSONOutputFormatParam{Schema: CopySchema()},
		},
	})
	defer stream.Close()

	res := Result{Model: a.cfg.Model, PromptVersion: CopyPromptVersion}
	var msg anthropic.Message
	var sb strings.Builder
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return res, fmt.Errorf("%w: accumulate: %v", ErrUpstream, err)
		}
		if e, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if d, ok := e.Delta.AsAny().(anthropic.TextDelta); ok {
				sb.WriteString(d.Text)
				onText(d.Text)
			}
		}
	}
	res.Text = sb.String()
	if err := stream.Err(); err != nil {
		return res, classify(ctx, err)
	}
	if msg.Model != "" {
		res.Model = string(msg.Model)
	}
	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens:
		return res, ErrTruncated
	case anthropic.StopReasonRefusal:
		return res, fmt.Errorf("%w: %s", ErrRefusal, msg.StopDetails.Category)
	}
	return res, nil
}
