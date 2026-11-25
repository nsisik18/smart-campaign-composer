package campaign

import (
	"context"
	"errors"
	"strings"

	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
)

// Event SSE'ye yazılacak tek olaydır (BLUEPRINT §4.6). Servis HTTP'yi
// bilmez; handler Event'leri text/event-stream'e çevirir.
type Event struct {
	Name string
	Data any
}

// Generation başlatılmış bir üretimdir: versiyon zaten artırılmıştır.
type Generation struct {
	campaign Campaign
	bounds   content.Bounds
}

func (g *Generation) Version() int { return g.campaign.Version }

// BeginGenerate ön koşulları kontrol eder ve versiyonu ayırır. Hata
// dönerse henüz stream açılmamıştır; handler normal JSON hata dönebilir.
func (s *Service) BeginGenerate(ctx context.Context, id int64) (*Generation, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Segment == nil || (c.Status != StatusAnalyzed && c.Status != StatusGenerated) {
		return nil, &ConflictError{Status: c.Status, Version: c.Version, Reason: "content can only be generated after audience analysis and before approval"}
	}
	reach, err := s.counter.Count(ctx, *c.Segment, s.clock.Now())
	if err != nil {
		return nil, err
	}
	if reach.Reachable == 0 {
		return nil, &ConflictError{Status: c.Status, Version: c.Version, Reason: "audience has no reachable recipients"}
	}
	b, err := s.bounds(ctx, *c.Segment)
	if err != nil {
		return nil, err
	}
	c, err = s.store.BeginGeneration(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Generation{campaign: c, bounds: b}, nil
}

// RunGenerate içeriği stream eder, doğrular, gerekirse TEK onarım dener
// ve kaydeder. Tüm sonuçlar emit ile bildirilir; terminal olay her zaman
// "validated" veya "failed"dır. Kısmi metin hiçbir zaman kaydedilmez.
func (s *Service) RunGenerate(ctx context.Context, g *Generation, emit func(Event)) {
	c := g.campaign
	emit(Event{"started", map[string]any{"version": c.Version}})

	req := llm.CopyRequest{
		Brief:               c.Brief,
		SegmentSummary:      c.Segment.Summary,
		UnsupportedCriteria: c.Segment.UnsupportedCriteria,
		MaxFirstNameLen:     content.RuneLen(g.bounds.LongestFirstName),
		MaxCityLen:          content.RuneLen(g.bounds.LongestCity),
	}

	ct, checks, issues, raw, err := s.streamOnce(ctx, req, g.bounds, emit)
	if err != nil {
		emit(failedEvent(err))
		return
	}
	if issues != nil {
		s.metrics.InvalidOutput(llm.StageCopy, "first")
		s.log.Info("copy invalid, repairing", "campaign_id", c.ID, "issues", contentIssueCodes(issues))
		emit(Event{"repairing", map[string]any{"errors": issues}})

		req.Repair = &llm.Repair{PreviousOutput: raw, Issues: contentIssueStrings(issues)}
		ct, checks, issues, _, err = s.streamOnce(ctx, req, g.bounds, emit)
		if err != nil {
			emit(failedEvent(err))
			return
		}
		if issues != nil {
			s.metrics.InvalidOutput(llm.StageCopy, "repair")
			emit(Event{"failed", map[string]any{
				"code": "invalid_output", "message": "AI çıktısı onarımdan sonra da doğrulamadan geçmedi",
				"errors": issues, "checks": checks,
			}})
			return
		}
	}

	saved, err := s.store.SaveContent(ctx, c.ID, c.Version, ct)
	if err != nil {
		emit(failedEvent(err))
		return
	}
	emit(Event{"validated", map[string]any{"content": ct, "checks": checks, "version": saved.Version}})
}

// streamOnce tek bir LLM çağrısını stream eder; delta'ları alan bazlı
// çözülmüş metin olarak iletir ve sonunda tam metni doğrular.
func (s *Service) streamOnce(ctx context.Context, req llm.CopyRequest, b content.Bounds, emit func(Event)) (content.Content, content.Checks, []content.Issue, string, error) {
	parser := llm.NewFieldStream("sms_text", "email_subject", "email_body")
	res, err := s.llm.StreamCopy(ctx, req, func(chunk string) {
		for _, d := range parser.Feed(chunk) {
			emit(Event{"delta", d})
		}
	})
	if err != nil {
		return content.Content{}, content.Checks{}, nil, res.Text, err
	}
	// Parser hatası yalnızca önizlemeyi durdurur; karar tam metnin katı
	// doğrulamasıyla verilir.
	if perr := parser.Err(); perr != nil {
		s.log.Info("stream preview parser stopped", "err", perr.Error())
	}
	ct, checks, issues := content.ParseAndValidate([]byte(res.Text), b)
	return ct, checks, issues, res.Text, nil
}

func failedEvent(err error) Event {
	code, msg := "internal", "beklenmeyen bir hata oluştu"
	var cf *ConflictError
	switch {
	case errors.As(err, &cf):
		code, msg = "superseded", "bu üretim daha yeni bir değişiklik nedeniyle geçersiz kaldı"
	case errors.Is(err, llm.ErrTruncated):
		code, msg = "llm_truncated", "AI çıktısı yarıda kesildi"
	case errors.Is(err, llm.ErrRefusal):
		code, msg = "llm_refusal", "AI bu isteği işlemeyi reddetti"
	case errors.Is(err, llm.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		code, msg = "llm_timeout", "AI zamanında cevap vermedi, tekrar deneyin"
	case errors.Is(err, llm.ErrUpstream):
		code, msg = "llm_unavailable", "AI servisine şu an ulaşılamıyor"
	}
	return Event{"failed", map[string]any{"code": code, "message": msg}}
}

func contentIssueStrings(is []content.Issue) []string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.String()
	}
	return out
}

func contentIssueCodes(is []content.Issue) string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.Code
	}
	return strings.Join(out, ",")
}
