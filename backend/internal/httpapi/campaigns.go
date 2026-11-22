package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
)

type campaignHandlers struct {
	svc        *campaign.Service
	statsEvery time.Duration
}

func (h campaignHandlers) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Brief string `json:"brief"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be {\"brief\": string}", nil)
		return
	}
	v, err := h.svc.Create(r.Context(), body.Brief)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h campaignHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h campaignHandlers) analyze(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Analyze(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// generate içerik üretimini SSE gövdeli POST olarak stream eder
// (BLUEPRINT §4.6). Ön koşul hataları stream açılmadan normal JSON döner.
func (h campaignHandlers) generate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	g, err := h.svc.BeginGenerate(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	sse := newSSE(w)
	stop := sse.keepalive(10 * time.Second)
	defer stop()
	h.svc.RunGenerate(r.Context(), g, func(e campaign.Event) { sse.send(e.Name, e.Data) })
}

func (h campaignHandlers) editContent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion *int `json:"expected_version"`
		content.Content
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.ExpectedVersion == nil {
		WriteError(w, http.StatusBadRequest, "invalid_input", "expected_version is required", nil)
		return
	}
	v, err := h.svc.EditContent(r.Context(), id, *body.ExpectedVersion, body.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h campaignHandlers) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion *int `json:"expected_version"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.ExpectedVersion == nil {
		WriteError(w, http.StatusBadRequest, "invalid_input", "expected_version is required", nil)
		return
	}
	res, err := h.svc.Approve(r.Context(), id, *body.ExpectedVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// events canlı pano akışıdır (GET + EventSource, BLUEPRINT §8):
//
//	event: snapshot   bağlanınca (ve yeniden bağlanınca) tam durum
//	event: stats      her 500 ms'de tam durum (delta değil: istemci
//	                  hiçbir olayı kaçırmaktan etkilenmez, reducer basit)
//	: heartbeat       15 s'de bir yorum (proxy'ler boşta kesmesin)
//
// Kampanya terminal duruma geçince son stats gönderilir ve akış kapanır;
// istemci terminal durumu görünce EventSource'u kendisi kapatır, yoksa
// EventSource otomatik yeniden bağlanır ve yeni snapshot alır.
func (h campaignHandlers) events(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	first, err := h.svc.Monitor(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	sse := newSSE(w)
	stop := sse.keepalive(15 * time.Second)
	defer stop()
	sse.send("snapshot", first)
	if first.Terminal() {
		return
	}
	tick := time.NewTicker(h.statsEvery)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			st, err := h.svc.Monitor(r.Context(), id)
			if err != nil {
				if r.Context().Err() == nil {
					sse.send("error", map[string]string{"code": "internal"})
				}
				return
			}
			sse.send("stats", st)
			if st.Terminal() {
				return
			}
		}
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "invalid request body: "+err.Error(), nil)
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "invalid_id", "campaign id must be a positive integer", nil)
		return 0, false
	}
	return id, true
}

// writeServiceError domain hatalarını BLUEPRINT §8'deki koda çevirir.
// Bilinmeyen hatalar 500 ve genel mesajdır; iç detay istemciye sızmaz.
func writeServiceError(w http.ResponseWriter, err error) {
	var (
		inErr  *campaign.InputError
		cfErr  *campaign.ConflictError
		outErr *campaign.InvalidOutputError
		ctErr  *campaign.InvalidContentError
	)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "campaign not found", nil)
	case errors.As(err, &inErr):
		WriteError(w, http.StatusBadRequest, "invalid_input", inErr.Msg, nil)
	case errors.As(err, &cfErr):
		WriteError(w, http.StatusConflict, "conflict", cfErr.Reason,
			[]any{map[string]any{"status": cfErr.Status, "version": cfErr.Version}})
	case errors.As(err, &outErr):
		details := make([]any, len(outErr.Issues))
		for i, is := range outErr.Issues {
			details[i] = is
		}
		WriteError(w, http.StatusUnprocessableEntity, "invalid_"+outErr.Stage,
			"AI çıktısı onarımdan sonra da doğrulamadan geçmedi", details)
	case errors.As(err, &ctErr):
		details := make([]any, len(ctErr.Issues))
		for i, is := range ctErr.Issues {
			details[i] = is
		}
		WriteError(w, http.StatusUnprocessableEntity, "invalid_content", "içerik kurallara uymuyor", details)
	case errors.Is(err, llm.ErrTruncated):
		WriteError(w, http.StatusBadGateway, "llm_truncated", "AI çıktısı yarıda kesildi", nil)
	case errors.Is(err, llm.ErrRefusal):
		WriteError(w, http.StatusUnprocessableEntity, "llm_refusal", "AI bu isteği işlemeyi reddetti; brief'i değiştirip tekrar deneyin", nil)
	case errors.Is(err, llm.ErrTimeout):
		WriteError(w, http.StatusBadGateway, "llm_timeout", "AI zamanında cevap vermedi, tekrar deneyin", nil)
	case errors.Is(err, llm.ErrUpstream):
		WriteError(w, http.StatusBadGateway, "llm_unavailable", "AI servisine şu an ulaşılamıyor", nil)
	default:
		WriteError(w, http.StatusInternalServerError, "internal", "internal error", nil)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
