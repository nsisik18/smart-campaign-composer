// Package campaign kampanya durum makinesi ve akışını yönetir:
// draft -> analyzed -> generated -> approved/sending -> completed*.
package campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/content"
)

type Status string

const (
	StatusDraft               Status = "draft"
	StatusAnalyzed            Status = "analyzed"
	StatusGenerated           Status = "generated"
	StatusApproved            Status = "approved"
	StatusSending             Status = "sending"
	StatusCompleted           Status = "completed"
	StatusCompletedWithErrors Status = "completed_with_errors"
)

type Campaign struct {
	ID        int64            `json:"id"`
	Brief     string           `json:"brief"`
	Status    Status           `json:"status"`
	Version   int              `json:"version"`
	Segment   *audience.Plan   `json:"segment"`
	Content   *content.Content `json:"content"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

const MaxBriefRunes = 2000

var ErrNotFound = errors.New("campaign not found")

// InputError kullanıcı girdisi hatası (400).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

// ConflictError yanlış durum veya versiyon (409). Mevcut durum istemciye
// döner ki UI kendini güncelleyebilsin.
type ConflictError struct {
	Status  Status
	Version int
	Reason  string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict: %s (status=%s version=%d)", e.Reason, e.Status, e.Version)
}

// InvalidOutputError Claude çıktısı onarımdan sonra da doğrulamadan geçmedi (422).
type InvalidOutputError struct {
	Stage  string
	Issues []audience.Issue
}

func (e *InvalidOutputError) Error() string {
	parts := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		parts[i] = is.String()
	}
	return e.Stage + " output invalid: " + strings.Join(parts, "; ")
}

// NoSupportedCriteriaError brief'in istediği kitle mevcut alanlarla ifade
// edilemiyor (422). Model bunu unsupported_criteria ile beyan etti; onarım
// denenmez. Kullanıcıya neyin desteklenmediği ve neyin desteklendiği gösterilir.
type NoSupportedCriteriaError struct {
	Criteria []string
}

func (e *NoSupportedCriteriaError) Error() string {
	return "no supported criteria: " + strings.Join(e.Criteria, "; ")
}
