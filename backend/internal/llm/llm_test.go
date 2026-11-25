package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
)

// Prompt, şema ve Go allowlist'i aynı alan/şehir listesini anlatmalı.
func TestPromptAndSchemaMatchAllowlist(t *testing.T) {
	p := SegmentPrompt()
	for _, f := range audience.FieldNames {
		if !strings.Contains(p, string(f)) {
			t.Errorf("prompt missing field %s", f)
		}
	}
	for _, c := range audience.Cities {
		if !strings.Contains(p, c) {
			t.Errorf("prompt missing city %s", c)
		}
	}
	enum := SegmentSchema()["properties"].(map[string]any)["rules"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["field"].(map[string]any)["enum"].([]string)
	if len(enum) != len(audience.FieldNames) {
		t.Fatalf("schema enum %v vs allowlist %v", enum, audience.FieldNames)
	}
	for i, f := range audience.FieldNames {
		if enum[i] != string(f) {
			t.Fatalf("schema enum order/content mismatch: %v", enum)
		}
	}
}

// Prompt'taki örnekler validator'dan geçmeli (son örnek hariç: bilerek boş kural).
func TestPromptExamplesAreValid(t *testing.T) {
	p := SegmentPrompt()
	var blocks []string
	for _, part := range strings.Split(p, "```json\n")[1:] {
		blocks = append(blocks, strings.SplitN(part, "\n```", 2)[0])
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 examples, got %d", len(blocks))
	}
	for i, b := range blocks[:2] {
		if _, issues := audience.ParseAndValidate([]byte(b)); issues != nil {
			t.Errorf("example %d invalid: %v", i, issues)
		}
	}
	if _, issues := audience.ParseAndValidate([]byte(blocks[2])); issues == nil {
		t.Error("injection example must be rejected by validator")
	}
}

func TestMockHeuristicExampleBrief(t *testing.T) {
	m := NewMock()
	res, err := m.Segment(context.Background(), SegmentRequest{Brief: "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."})
	if err != nil {
		t.Fatal(err)
	}
	p, issues := audience.ParseAndValidate([]byte(res.Text))
	if issues != nil {
		t.Fatalf("%v\n%s", issues, res.Text)
	}
	if len(p.Rules) != 3 || p.Rules[0].Value != "istanbul" {
		t.Fatalf("unexpected plan %+v", p)
	}
}

func TestMockFixtures(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	for name, f := range segmentFixtures {
		res, err := m.Segment(ctx, SegmentRequest{Brief: "x [[fixture:" + name + "]]"})
		if f.err != nil {
			if !errors.Is(err, f.err) {
				t.Errorf("%s: err=%v", name, err)
			}
			continue
		}
		_, issues := audience.ParseAndValidate([]byte(res.Text))
		if (name == "valid") != (issues == nil) {
			t.Errorf("%s: issues=%v", name, issues)
		}
	}
	if _, err := m.Segment(ctx, SegmentRequest{Brief: "[[fixture:nope]]"}); err == nil {
		t.Error("unknown fixture should error")
	}
}

// Anthropic istemcisini sahte API'ye karşı çalıştırır: istek şekli
// (structured output, system prompt, onarım mesajları) ve cevap eşlemesi.
func TestAnthropicRequestAndResponse(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, messageJSON("end_turn", `{\"summary\":\"x\",\"rules\":[],\"unsupported_criteria\":[]}`))
	}))
	defer ts.Close()

	a := NewAnthropic(AnthropicConfig{APIKey: "test", Model: "claude-sonnet-5-5", Timeout: 5 * time.Second, BaseURL: ts.URL})
	res, err := a.Segment(context.Background(), SegmentRequest{
		Brief:  "brief metni",
		Repair: &Repair{PreviousOutput: `{"bad":1}`, Issues: []string{"rules[0].field: bilinmeyen alan"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, `"summary":"x"`) || res.PromptVersion != SegmentPromptVersion {
		t.Fatalf("res %+v", res)
	}
	oc := gotBody["output_config"].(map[string]any)
	if oc["format"].(map[string]any)["type"] != "json_schema" || oc["effort"] != "low" {
		t.Fatalf("output_config %v", oc)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("repair request should have user/assistant/user, got %d", len(msgs))
	}
	last, _ := json.Marshal(msgs[2])
	if !strings.Contains(string(last), "bilinmeyen alan") {
		t.Fatalf("repair issues not sent: %s", last)
	}
	sys, _ := json.Marshal(gotBody["system"])
	if !strings.Contains(string(sys), "unsupported_criteria") {
		t.Fatal("system prompt not sent")
	}
}

func TestAnthropicStopReasonsAndErrors(t *testing.T) {
	var calls atomic.Int32
	mode := "max_tokens"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
		case "refusal":
			_, _ = io.WriteString(w, messageJSON("refusal", ""))
		default:
			_, _ = io.WriteString(w, messageJSON("max_tokens", `{\"summary\":`))
		}
	}))
	defer ts.Close()
	a := NewAnthropic(AnthropicConfig{APIKey: "test", Model: "m", Timeout: 10 * time.Second, BaseURL: ts.URL})

	if _, err := a.Segment(context.Background(), SegmentRequest{Brief: "b"}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("max_tokens -> %v", err)
	}
	mode = "refusal"
	if _, err := a.Segment(context.Background(), SegmentRequest{Brief: "b"}); !errors.Is(err, ErrRefusal) {
		t.Fatalf("refusal -> %v", err)
	}
	mode = "500"
	calls.Store(0)
	if _, err := a.Segment(context.Background(), SegmentRequest{Brief: "b"}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("500 -> %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("5xx should be retried exactly once, calls=%d", calls.Load())
	}
}

func messageJSON(stop, text string) string {
	content := `[]`
	if text != "" {
		content = `[{"type":"text","text":"` + text + `"}]`
	}
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":` + content +
		`,"stop_reason":"` + stop + `","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
}

// Gerçek Anthropic streaming formatını taklit eden sunucu: StreamCopy her
// text_delta'yı sırayla iletmeli, stop_reason'ı doğru eşlemeli.
func TestAnthropicStreamCopy(t *testing.T) {
	stop := "end_turn"
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		ev := func(name, data string) { _, _ = io.WriteString(w, "event: "+name+"\ndata: "+data+"\n\n") }
		ev("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`)
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		for _, part := range []string{`{\"sms_text\":\"Mer`, `haba\\n\",\"email_subject\":\"Konu\",`, `\"email_body\":\"Gövde\"}`} {
			ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+part+`"}}`)
		}
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
		ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stop+`","stop_sequence":null},"usage":{"output_tokens":20}}`)
		ev("message_stop", `{"type":"message_stop"}`)
	}))
	defer ts.Close()

	a := NewAnthropic(AnthropicConfig{APIKey: "test", Model: "claude-sonnet-5-5", Timeout: 5 * time.Second, BaseURL: ts.URL})
	var chunks []string
	res, err := a.StreamCopy(context.Background(), CopyRequest{Brief: "b", SegmentSummary: "s", MaxFirstNameLen: 15, MaxCityLen: 8},
		func(s string) { chunks = append(chunks, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 || res.Text != strings.Join(chunks, "") {
		t.Fatalf("chunks %q text %q", chunks, res.Text)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(res.Text), &m); err != nil || m["sms_text"] != "Merhaba\n" {
		t.Fatalf("%v %v", m, err)
	}
	if gotBody["stream"] != true || gotBody["output_config"].(map[string]any)["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("request %v", gotBody)
	}
	user, _ := json.Marshal(gotBody["messages"])
	if !strings.Contains(string(user), "en uzun {{first_name}} değeri 15 karakter") {
		t.Fatalf("render bounds not sent to the model: %s", user)
	}

	stop = "max_tokens"
	if _, err := a.StreamCopy(context.Background(), CopyRequest{Brief: "b"}, func(string) {}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("max_tokens -> %v", err)
	}
}

func TestMockHeuristicCitySuffixes(t *testing.T) {
	for _, brief := range []string{"İstanbullulara sepet hatırlatması", "Istanbul'daki sepet", "ISTANBUL sepet", "izmirlilere ve Ankaralılara sepet"} {
		res, _ := NewMock().Segment(context.Background(), SegmentRequest{Brief: brief})
		p, issues := audience.ParseAndValidate([]byte(res.Text))
		if issues != nil || p.Rules[0].Field != audience.FieldCity {
			t.Errorf("%q: %v %s", brief, issues, res.Text)
		}
	}
}

// Hiçbir kriter tanınmayan brief: mock kural uydurmaz, beyan eder.
func TestMockDeclaresUnsupportedWhenNothingMatches(t *testing.T) {
	res, _ := NewMock().Segment(context.Background(), SegmentRequest{Brief: "son 30 günde istnabulkart kullananlara %30 indirim"})
	_, issues := audience.ParseAndValidate([]byte(res.Text))
	crit, ok := audience.UnsupportedOnly([]byte(res.Text), issues)
	if !ok || len(crit) != 1 || !strings.Contains(crit[0], "istnabulkart") {
		t.Fatalf("%v %v %s", crit, issues, res.Text)
	}
}

// Gerçek Claude testinde bulundu: şema map olarak gidince properties
// alfabetik sıralanıyor ve model önce email_body üretiyordu. SDK'nın
// gerçekten gönderdiği gövdede sıra sms_text, email_subject, email_body
// olmalı; uygulanamayan kriterler de kullanıcı mesajında olmalı.
func TestAnthropicCopyRequestOrderAndUnsupported(t *testing.T) {
	var raw string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest) // retry edilmez; yalnızca isteği görmek istiyoruz
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"x"}}`)
	}))
	defer ts.Close()

	a := NewAnthropic(AnthropicConfig{APIKey: "test", Model: "claude-sonnet-5-5", Timeout: 5 * time.Second, BaseURL: ts.URL})
	_, _ = a.StreamCopy(context.Background(), CopyRequest{
		Brief: "b", SegmentSummary: "İzmir", UnsupportedCriteria: []string{"VIP müşteriler"},
		MaxFirstNameLen: 15, MaxCityLen: 8,
	}, func(string) {})

	i, j, k := strings.Index(raw, `"sms_text"`), strings.Index(raw, `"email_subject"`), strings.Index(raw, `"email_body"`)
	if i < 0 || !(i < j && j < k) {
		t.Fatalf("schema property order must be sms_text, email_subject, email_body; got %d %d %d", i, j, k)
	}
	if !strings.Contains(raw, "VIP") || !strings.Contains(raw, "Uygulanamayan kriterler") {
		t.Fatal("unsupported criteria not sent to the copy prompt")
	}
	if !strings.Contains(raw, "ek getirme") {
		t.Fatal("copy_v2 system prompt not sent")
	}
}
