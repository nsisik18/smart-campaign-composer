package campaign

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

// Store ve Counter servisin bağımlılıklarıdır; unit testlerde sahteleri verilir.
type Store interface {
	Create(ctx context.Context, brief string) (Campaign, error)
	Get(ctx context.Context, id int64) (Campaign, error)
	SaveSegment(ctx context.Context, id int64, expectedVersion int, p audience.Plan) (Campaign, error)
	BeginGeneration(ctx context.Context, id int64) (Campaign, error)
	SaveContent(ctx context.Context, id int64, genVersion int, c content.Content) (Campaign, error)
	UpdateContent(ctx context.Context, id int64, expectedVersion int, c content.Content) (Campaign, error)
	Approve(ctx context.Context, id int64, expectedVersion int, now time.Time) (Campaign, QueuedCounts, error)
	DeliveryTotals(ctx context.Context, id int64) (QueuedCounts, error)
	MonitorStats(ctx context.Context, id int64) (MonitorStats, error)
}

// Counter kitle sorgularıdır (audience.Repository).
type Counter interface {
	Count(ctx context.Context, p audience.Plan, now time.Time) (audience.Reach, error)
	RenderBounds(ctx context.Context, p audience.Plan, now time.Time) (string, []string, error)
}

type Service struct {
	store   Store
	counter Counter
	llm     llm.Provider
	metrics *llm.Metrics
	clock   platform.Clock
	log     *slog.Logger
}

func NewService(store Store, counter Counter, provider llm.Provider, m *llm.Metrics, clock platform.Clock, log *slog.Logger) *Service {
	return &Service{store: store, counter: counter, llm: provider, metrics: m, clock: clock, log: log}
}

// View kampanya + (segment varsa) canlı hesaplanmış erişim sayıları.
type View struct {
	Campaign Campaign        `json:"campaign"`
	Reach    *audience.Reach `json:"reach"`
	// Checks kayıtlı içeriğin uzunluk özeti (içerik varsa).
	Checks *content.Checks `json:"checks"`
	// Queued onaydan sonra kuyruğa alınan teslimat sayıları.
	Queued *QueuedCounts `json:"queued"`
}

type AnalyzeResult struct {
	View
	Repaired bool `json:"repaired"`
}

func (s *Service) Create(ctx context.Context, brief string) (View, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return View{}, &InputError{Msg: "brief boş olamaz"}
	}
	if utf8.RuneCountInString(brief) > MaxBriefRunes {
		return View{}, &InputError{Msg: "brief en fazla 2000 karakter olabilir"}
	}
	c, err := s.store.Create(ctx, brief)
	return View{Campaign: c}, err
}

func (s *Service) Get(ctx context.Context, id int64) (View, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, c)
}

// Analyze brief'ten segment üretir, doğrular, gerekirse TEK onarım dener,
// sayar ve kaydeder. Geçersiz çıktı hiçbir aşamada SQL'e ulaşmaz: Count
// yalnızca ParseAndValidate'ten hatasız dönmüş bir Plan ile çağrılır.
func (s *Service) Analyze(ctx context.Context, id int64) (AnalyzeResult, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return AnalyzeResult{}, err
	}
	if c.Status != StatusDraft && c.Status != StatusAnalyzed {
		return AnalyzeResult{}, &ConflictError{Status: c.Status, Version: c.Version, Reason: "segment can only be (re)analyzed before content generation"}
	}

	plan, repaired, err := s.segmentWithRepair(ctx, c)
	if err != nil {
		return AnalyzeResult{}, err
	}

	saved, err := s.store.SaveSegment(ctx, c.ID, c.Version, plan)
	if err != nil {
		return AnalyzeResult{}, err
	}
	v, err := s.view(ctx, saved)
	return AnalyzeResult{View: v, Repaired: repaired}, err
}

func (s *Service) segmentWithRepair(ctx context.Context, c Campaign) (audience.Plan, bool, error) {
	res, err := s.llm.Segment(ctx, llm.SegmentRequest{Brief: c.Brief})
	if err != nil {
		// Taşıma hataları (timeout, 5xx, kesilme, ret) onarılmaz: SDK zaten
		// tek retry yaptı; kesilmiş çıktıyı onarmaya çalışmak anlamsız.
		return audience.Plan{}, false, err
	}
	plan, issues := audience.ParseAndValidate([]byte(res.Text))
	if issues == nil {
		return plan, false, nil
	}

	s.metrics.InvalidOutput(llm.StageSegment, "first")
	s.log.Info("segment invalid, repairing", "campaign_id", c.ID, "issues", issueCodes(issues))

	res2, err := s.llm.Segment(ctx, llm.SegmentRequest{
		Brief:  c.Brief,
		Repair: &llm.Repair{PreviousOutput: res.Text, Issues: issueStrings(issues)},
	})
	if err != nil {
		return audience.Plan{}, false, err
	}
	plan, issues = audience.ParseAndValidate([]byte(res2.Text))
	if issues != nil {
		s.metrics.InvalidOutput(llm.StageSegment, "repair")
		return audience.Plan{}, false, &InvalidOutputError{Stage: "segment", Issues: issues}
	}
	return plan, true, nil
}

func (s *Service) view(ctx context.Context, c Campaign) (View, error) {
	v := View{Campaign: c}
	if c.Segment != nil {
		r, err := s.counter.Count(ctx, *c.Segment, s.clock.Now())
		if err != nil {
			return View{}, err
		}
		v.Reach = &r
	}
	if c.Content != nil && c.Segment != nil {
		b, err := s.bounds(ctx, *c.Segment)
		if err != nil {
			return View{}, err
		}
		_, ch, _ := content.Validate(*c.Content, b)
		v.Checks = &ch
	}
	if c.Status == StatusSending || c.Status == StatusCompleted || c.Status == StatusCompletedWithErrors {
		q, err := s.store.DeliveryTotals(ctx, c.ID)
		if err != nil {
			return View{}, err
		}
		v.Queued = &q
	}
	return v, nil
}

// bounds kitledeki en uzun ad ve en uzun şehir görünen adı.
func (s *Service) bounds(ctx context.Context, p audience.Plan) (content.Bounds, error) {
	name, cities, err := s.counter.RenderBounds(ctx, p, s.clock.Now())
	if err != nil {
		return content.Bounds{}, err
	}
	b := content.Bounds{LongestFirstName: name}
	for _, k := range cities {
		if d := audience.CityDisplay(k); content.RuneLen(d) > content.RuneLen(b.LongestCity) {
			b.LongestCity = d
		}
	}
	return b, nil
}

func issueStrings(is []audience.Issue) []string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.String()
	}
	return out
}

func issueCodes(is []audience.Issue) []string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.Code
	}
	return out
}

// EditContent insan düzenlemesini Claude çıktısıyla AYNI validator'dan
// geçirir ve yalnızca geçerliyse yazar. Geçersiz düzenleme kaydedilmez;
// frontend taslağı tutar ve alan bazlı hatayı gösterir.
func (s *Service) EditContent(ctx context.Context, id int64, expectedVersion int, in content.Content) (View, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	if c.Status != StatusGenerated || c.Version != expectedVersion || c.Segment == nil {
		return View{}, &ConflictError{Status: c.Status, Version: c.Version, Reason: "content is editable only in generated state at the version you are editing"}
	}
	b, err := s.bounds(ctx, *c.Segment)
	if err != nil {
		return View{}, err
	}
	ct, _, issues := content.Validate(in, b)
	if issues != nil {
		return View{}, &InvalidContentError{Issues: issues}
	}
	saved, err := s.store.UpdateContent(ctx, id, expectedVersion, ct)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, saved)
}

type ApproveResult struct {
	View
	Queued QueuedCounts `json:"queued"`
}

func (s *Service) Approve(ctx context.Context, id int64, expectedVersion int) (ApproveResult, error) {
	c, q, err := s.store.Approve(ctx, id, expectedVersion, s.clock.Now())
	if err != nil {
		return ApproveResult{}, err
	}
	s.log.Info("campaign approved", "campaign_id", c.ID, "queued_sms", q.SMS, "queued_email", q.Email)
	v, err := s.view(ctx, c)
	return ApproveResult{View: v, Queued: q}, err
}

// Monitor panonun anlık görüntüsü.
func (s *Service) Monitor(ctx context.Context, id int64) (MonitorStats, error) {
	return s.store.MonitorStats(ctx, id)
}
