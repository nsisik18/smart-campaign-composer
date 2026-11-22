// claudeprobe: Gün 1 doğrulama denemesi (BLUEPRINT §4.2).
//
// Soru: Go SDK ile `output_config.format = json_schema` VE streaming aynı
// istekte birlikte çalışıyor mu; text delta'lar JSON metnini gerçekten
// parça parça mı getiriyor (artımlı parser'ın varsayımı), stop_reason ve
// son JSON beklenen şekilde mi?
//
// Ürün kodu değildir; Gün 3'te internal/llm'deki anthropic istemcisi bu
// denemenin sonuçlarına göre yazılır. Çalıştırma:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/claudeprobe [-schema copy|segment] [-effort low]
//	go run ./cmd/claudeprobe -check-maxlength   # API maxLength'i reddediyor mu?
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/insider-case/smart-campaign-composer/internal/llm"
)

const brief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

func main() {
	schemaName := flag.String("schema", "copy", "copy | segment")
	effort := flag.String("effort", "low", "low | medium | high")
	checkMax := flag.Bool("check-maxlength", false, "send maxLength in schema to see if the API rejects it")
	flag.Parse()

	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	client := anthropic.NewClient(option.WithMaxRetries(1)) // BLUEPRINT: tek retry

	var schema map[string]any
	var system, user string
	switch *schemaName {
	case "segment":
		// Ürün kodundaki şema ve prompt birebir kullanılır; probe ayrı kopya tutmaz.
		schema = llm.SegmentSchema()
		system = llm.SegmentPrompt()
		user = "<brief>" + brief + "</brief>"
	default:
		schema = llm.CopySchema()
		if *checkMax {
			schema["properties"].(map[string]any)["sms_text"].(map[string]any)["maxLength"] = 160
		}
		system = llm.CopyPrompt()
		user = "<brief>" + brief + "</brief>\n\nHedef kitle: Son 30 günde sepete ekleyip almayan İstanbullu kullanıcılar\n\nRender sınırı: kitledeki en uzun {{first_name}} değeri 15 karakter, en uzun {{city}} değeri 8 karakter."
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(*effort),
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	stream := client.Messages.NewStreaming(ctx, params)
	var msg anthropic.Message
	var firstText time.Duration
	textDeltas, thinkingDeltas := 0, 0
	var raw strings.Builder

	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			fail("accumulate: %v", err)
		}
		if e, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			switch d := e.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if textDeltas == 0 {
					firstText = time.Since(start)
				}
				textDeltas++
				raw.WriteString(d.Text)
				fmt.Printf("[%5dms delta #%02d] %q\n", time.Since(start).Milliseconds(), textDeltas, d.Text)
			case anthropic.ThinkingDelta:
				thinkingDeltas++
			}
		}
	}
	if err := stream.Err(); err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			fail("API error %d: %s", apiErr.StatusCode, apiErr.Error())
		}
		fail("stream error: %v", err)
	}

	fmt.Println("\n── Sonuç ──")
	fmt.Printf("model=%s stop_reason=%s total=%s first_text=%s text_deltas=%d thinking_deltas=%d\n",
		msg.Model, msg.StopReason, time.Since(start).Round(time.Millisecond), firstText.Round(time.Millisecond),
		textDeltas, thinkingDeltas)
	fmt.Printf("usage: in=%d out=%d\n", msg.Usage.InputTokens, msg.Usage.OutputTokens)

	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens:
		fail("truncated output (max_tokens): ürün kodunda ayrı hata kodu")
	case anthropic.StopReasonRefusal:
		fail("refusal: category=%s", msg.StopDetails.Category)
	}

	// Accumulate edilen text ile delta'ların birleşimi aynı olmalı.
	var final strings.Builder
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			final.WriteString(t.Text)
		}
	}
	if final.String() != raw.String() {
		fail("accumulated text != concatenated deltas")
	}

	dec := json.NewDecoder(bytes.NewReader([]byte(raw.String())))
	dec.DisallowUnknownFields()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		fail("final JSON invalid: %v", err)
	}
	pretty, _ := json.MarshalIndent(out, "", "  ")
	fmt.Printf("valid JSON:\n%s\n", pretty)

	if *schemaName == "copy" {
		for _, k := range []string{"sms_text", "email_subject"} {
			s, _ := out[k].(string)
			fmt.Printf("%s: %d rune (render öncesi)\n", k, len([]rune(s)))
		}
	}
	if textDeltas < 2 {
		fmt.Println("UYARI: tek delta geldi; artımlı alan streaming'i bu cevapta gözlenemedi")
	} else {
		fmt.Println("OK: structured output + streaming birlikte çalışıyor, JSON parça parça geldi")
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+f+"\n", a...)
	os.Exit(1)
}
