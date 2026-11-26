// Package llm Claude ile konuşan katmandır. Claude öneri üretir, karar
// vermez: bu paket yalnızca ham metin döner; doğrulama audience/content
// paketlerinde, yani güvenilir tarafta yapılır.
package llm

import (
	"context"
	_ "embed"
	"errors"
)

// Prompt'lar binary'ye gömülü; değişiklik = yeni versiyon dosyası.
//
//go:embed prompts/segment_v1.md
var segmentPromptV1 string

const SegmentPromptVersion = "segment_v1"

//go:embed prompts/copy_v2.md
var copyPrompt string

// copy_v2: placeholder'a ek yasağı ve uygulanamayan kriterler (gerçek
// Claude ile yapılan testte bulunan iki hata, DECISIONS ADR-16/17).
const CopyPromptVersion = "copy_v2"

func CopyPrompt() string { return copyPrompt }

// SegmentPrompt testlerin prompt/allowlist senkronunu denetleyebilmesi için.
func SegmentPrompt() string { return segmentPromptV1 }

type Stage string

const (
	StageSegment Stage = "segment"
	StageCopy    Stage = "copy"
)

// Repair bir önceki çıktıyı ve backend'in makine tarafından ürettiği hata
// listesini taşır. Nil değilse çağrı onarım çağrısıdır (tek deneme, §4.5).
type Repair struct {
	PreviousOutput string
	Issues         []string
}

type SegmentRequest struct {
	Brief  string
	Repair *Repair
}

type Result struct {
	Text          string
	Model         string
	PromptVersion string
}

// CopyRequest içerik üretimi girdisi. Müşteri verisi YOK: yalnızca brief,
// segment özeti ve render sınırı için en uzun değerlerin UZUNLUKLARI.
type CopyRequest struct {
	Brief          string
	SegmentSummary string
	// UnsupportedCriteria brief'te olup kitleye uygulanamayan kriterler.
	// Metin bu kitleye özelmiş gibi yazılmasın diye prompt'a gider.
	UnsupportedCriteria []string
	MaxFirstNameLen     int
	MaxCityLen          int
	Repair              *Repair
}

// Provider gerçek Claude ve mock için ortak arayüz.
type Provider interface {
	Segment(ctx context.Context, req SegmentRequest) (Result, error)
	// StreamCopy içerik JSON'unu üretirken her ham text parçasını onText'e
	// verir (sıralı, aynı goroutine). Dönüşte Result.Text tam metindir.
	// Hata durumunda o ana kadar gelen parçalar yine onText'e verilmiştir.
	StreamCopy(ctx context.Context, req CopyRequest, onText func(string)) (Result, error)
}

// Hata sınıfları. Servis bunları HTTP koduna ve kullanıcı mesajına çevirir.
var (
	ErrTruncated = errors.New("llm: output truncated (max_tokens)")
	ErrRefusal   = errors.New("llm: model refused")
	ErrTimeout   = errors.New("llm: timeout")
	ErrUpstream  = errors.New("llm: upstream error")
)

// Outcome metrik etiketi için hatayı sınıflandırır.
func Outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrTruncated):
		return "truncated"
	case errors.Is(err, ErrRefusal):
		return "refusal"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	default:
		return "upstream_error"
	}
}
