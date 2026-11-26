package campaign

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
)

// analyzed durumda, segmenti olan bir kampanya ile servis kurar.
func newGenSvc(brief string) (*Service, *fakeStore, *fakeCounter, *llm.Mock) {
	svc, st, ct, m := newSvc(brief)
	p := audience.Plan{Summary: "İstanbullular", Rules: []audience.Rule{{Field: audience.FieldCity, Op: audience.OpEq, Value: "istanbul"}}, UnsupportedCriteria: []string{}}
	st.c.Segment, st.c.Status, st.c.Version = &p, StatusAnalyzed, 2
	return svc, st, ct, m
}

type recorder struct{ events []Event }

func (r *recorder) emit(e Event) { r.events = append(r.events, e) }

func (r *recorder) names() []string {
	var out []string
	for _, e := range r.events {
		if len(out) == 0 || out[len(out)-1] != e.Name || e.Name != "delta" {
			out = append(out, e.Name)
		}
	}
	return out
}

func (r *recorder) last() Event { return r.events[len(r.events)-1] }

func (r *recorder) failCode() string {
	if e := r.last(); e.Name == "failed" {
		return e.Data.(map[string]any)["code"].(string)
	}
	return ""
}

func (r *recorder) failIssues() []string {
	var out []string
	if is, ok := r.last().Data.(map[string]any)["errors"].([]content.Issue); ok {
		for _, i := range is {
			out = append(out, i.Path+":"+i.Code)
		}
	}
	return out
}

func run(t *testing.T, svc *Service) *recorder {
	t.Helper()
	g, err := svc.BeginGenerate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	svc.RunGenerate(context.Background(), g, rec.emit)
	return rec
}

func TestGenerateValidStreamsAndSaves(t *testing.T) {
	svc, st, _, m := newGenSvc("[[copy:valid]]")
	rec := run(t, svc)
	if got := rec.names(); !slices.Equal(got, []string{"started", "delta", "validated"}) {
		t.Fatalf("events %v", got)
	}
	if rec.events[0].Data.(map[string]any)["version"] != 3 {
		t.Fatalf("started should carry the reserved version: %+v", rec.events[0])
	}
	// Delta'ların birleşimi kaydedilen içerikle birebir aynı.
	fields := map[string]string{}
	deltas := 0
	for _, e := range rec.events {
		if d, ok := e.Data.(llm.FieldDelta); ok {
			fields[d.Field] += d.Text
			deltas++
		}
	}
	if deltas < 10 {
		t.Fatalf("expected many incremental deltas, got %d", deltas)
	}
	sc := st.savedContent
	if sc == nil || fields["sms_text"] != sc.SMSText || fields["email_subject"] != sc.EmailSubject || fields["email_body"] != sc.EmailBody {
		t.Fatalf("deltas != saved content\n%+v\n%+v", fields, sc)
	}
	v := rec.last().Data.(map[string]any)
	if v["version"] != 4 || st.c.Status != StatusGenerated || m.Repairs() != 0 {
		t.Fatalf("%+v status=%s", v, st.c.Status)
	}
	if ch := v["checks"].(content.Checks); !ch.SMS.OK || ch.PreviewVars.FirstName != "Abdurrahmangazi" || ch.PreviewVars.City != "İstanbul" {
		t.Fatalf("checks %+v", ch)
	}
}

// AI-04: bozuk JSON ve kesilme -> başarısız, kayıt yok (onay için içerik yok).
func TestGenerateBrokenOutputFails(t *testing.T) {
	svc, st, _, m := newGenSvc("[[copy:invalid_json]]")
	rec := run(t, svc)
	if rec.failCode() != "invalid_output" || !slices.Contains(rec.failIssues(), "$:invalid_json") || st.savedContent != nil || m.Repairs() != 1 {
		t.Fatalf("code=%s issues=%v saved=%v", rec.failCode(), rec.failIssues(), st.savedContent)
	}

	svc, st, _, m = newGenSvc("[[copy:truncated]]")
	rec = run(t, svc)
	if rec.failCode() != "llm_truncated" || st.savedContent != nil || m.Calls() != 1 {
		t.Fatalf("truncated: code=%s calls=%d", rec.failCode(), m.Calls())
	}
	if st.c.Status != StatusAnalyzed {
		t.Fatalf("status must stay analyzed, got %s", st.c.Status)
	}
	// Kısmi metin yine de stream edildi (kullanıcı ne olduğunu görür) ama kaydedilmedi.
	if !slices.Contains(rec.names(), "delta") {
		t.Fatal("partial deltas expected before failure")
	}
}

// AI-05: ilk çıktı geçersiz, onarım geçerli; tek onarım çağrısı.
func TestGenerateRepair(t *testing.T) {
	svc, st, _, m := newGenSvc("[[copy:invalid_then_repaired]]")
	rec := run(t, svc)
	if got := rec.names(); !slices.Equal(got, []string{"started", "delta", "repairing", "delta", "validated"}) {
		t.Fatalf("events %v", got)
	}
	if m.Repairs() != 1 || m.Calls() != 2 || st.savedContent == nil {
		t.Fatalf("calls=%d repairs=%d", m.Calls(), m.Repairs())
	}
	errs := rec.events[slices.IndexFunc(rec.events, func(e Event) bool { return e.Name == "repairing" })].Data.(map[string]any)["errors"].([]content.Issue)
	if errs[0].Code != "too_long" {
		t.Fatalf("repair should carry machine-readable issues: %+v", errs)
	}
}

// AI-06 / AI-07: render sonrası uzunluk ve bilinmeyen placeholder.
func TestGenerateRuleViolations(t *testing.T) {
	cases := map[string]string{
		"overlong_sms":                  "sms_text:too_long",
		"overlong_subject_after_render": "email_subject:too_long",
		"unknown_placeholder":           "sms_text:unknown_placeholder",
	}
	for fx, want := range cases {
		svc, st, _, _ := newGenSvc("[[copy:" + fx + "]]")
		rec := run(t, svc)
		if rec.failCode() != "invalid_output" || !slices.Contains(rec.failIssues(), want) || st.savedContent != nil {
			t.Errorf("%s: code=%s issues=%v", fx, rec.failCode(), rec.failIssues())
		}
	}
}

// overlong_subject_after_render fixture'ı gerçekten "şablon sığıyor, render
// sığmıyor" sınırında olmalı; yoksa test yanlış şeyi kanıtlar.
func TestSubjectFixtureIsOnTheBoundary(t *testing.T) {
	m := llm.NewMock()
	res, _ := m.StreamCopy(context.Background(), llm.CopyRequest{Brief: "[[copy:overlong_subject_after_render]]"}, func(string) {})
	_, ch, _ := content.ParseAndValidate([]byte(res.Text), content.Bounds{LongestFirstName: "Abdurrahmangazi", LongestCity: "İstanbul"})
	if ch.Subject.TemplateLength > content.SubjectLimit || ch.Subject.RenderedMax <= content.SubjectLimit {
		t.Fatalf("fixture not on boundary: %+v", ch.Subject)
	}
}

// Eski stream'in sonucu daha yeni bir üretimin üzerine yazamaz.
func TestGenerateSuperseded(t *testing.T) {
	svc, st, _, _ := newGenSvc("[[copy:valid]]")
	g1, err := svc.BeginGenerate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginGenerate(context.Background(), 1); err != nil { // ikinci sekme
		t.Fatal(err)
	}
	rec := &recorder{}
	svc.RunGenerate(context.Background(), g1, rec.emit)
	if rec.failCode() != "superseded" || st.savedContent != nil {
		t.Fatalf("code=%s", rec.failCode())
	}
}

func TestGenerateTransportErrors(t *testing.T) {
	for fx, code := range map[string]string{"timeout": "llm_timeout", "refusal": "llm_refusal"} {
		svc, st, _, m := newGenSvc("[[copy:" + fx + "]]")
		rec := run(t, svc)
		if rec.failCode() != code || m.Calls() != 1 || st.savedContent != nil {
			t.Errorf("%s: code=%s calls=%d", fx, rec.failCode(), m.Calls())
		}
	}
}

func TestBeginGeneratePreconditions(t *testing.T) {
	var cf *ConflictError
	svc, st, _, m := newGenSvc("[[copy:valid]]")
	st.c.Status = StatusDraft
	if _, err := svc.BeginGenerate(context.Background(), 1); !errors.As(err, &cf) {
		t.Fatalf("draft: %v", err)
	}
	svc, _, ct, _ := newGenSvc("[[copy:valid]]")
	ct.reachable = 0
	if _, err := svc.BeginGenerate(context.Background(), 1); !errors.As(err, &cf) || !strings.Contains(cf.Reason, "reachable") {
		t.Fatalf("unreachable: %v", err)
	}
	if m.Calls() != 0 {
		t.Fatal("no LLM call before preconditions pass")
	}
}

// Model teklifi değiştirir, link ve kupon uydurursa onarım tetiklenir.
func TestGenerateFidelityRepair(t *testing.T) {
	svc, st, _, m := newGenSvc("Sepete ekleyip almayanlara %30 indirim [[copy:invented_offer]]")
	rec := run(t, svc)
	if got := rec.names(); !slices.Equal(got, []string{"started", "delta", "repairing", "delta", "validated"}) {
		t.Fatalf("events %v", got)
	}
	errs := rec.events[slices.IndexFunc(rec.events, func(e Event) bool { return e.Name == "repairing" })].Data.(map[string]any)["errors"].([]content.Issue)
	var got []string
	for _, e := range errs {
		got = append(got, e.Code)
	}
	for _, want := range []string{"offer_mismatch", "invented_code", "invented_link", "offer_missing"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if m.Repairs() != 1 || st.savedContent == nil || !strings.Contains(st.savedContent.SMSText, "%30") {
		t.Fatalf("repairs=%d saved=%+v", m.Repairs(), st.savedContent)
	}
}
