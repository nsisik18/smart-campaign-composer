package llm

// SegmentSchema structured output için JSON şeması.
//
// Notlar (BLUEPRINT §4.2):
//   - Tüm objelerde additionalProperties:false ve tüm alanlar required.
//   - Uzunluk/aralık kuralları şemaya konmaz (API maxLength/minimum gibi
//     kısıtları desteklemiyor); zorlama tamamen audience.Validate'te.
//   - field/op enum'ları şemada var ama Go yine allowlist kontrolü yapar:
//     şema yapıyı garanti eder, iş kurallarını Go garanti eder.
//   - value tipi alana göre değiştiği için anyOf. Hangi alanın hangi tipi
//     aldığı şemada ifade edilmez; Validate denetler.
func SegmentSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string", "description": "Hedef kitlenin tek cümlelik Türkçe özeti, en fazla 300 karakter."},
			"rules": map[string]any{
				"type":        "array",
				"description": "AND ile birleşen 1-10 kural.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"field": map[string]any{"type": "string", "enum": []string{
							"city", "cart_added_within_days", "purchased_since_last_cart", "purchased_within_days", "total_orders"}},
						"op": map[string]any{"type": "string", "enum": []string{"eq", "in", "lte", "gte", "gt"}},
						"value": map[string]any{"anyOf": []any{
							map[string]any{"type": "string"},
							map[string]any{"type": "integer"},
							map[string]any{"type": "boolean"},
							map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						}},
					},
					"required":             []string{"field", "op", "value"},
					"additionalProperties": false,
				},
			},
			"unsupported_criteria": map[string]any{
				"type":        "array",
				"description": "Kural diliyle ifade edilemeyen kriterler.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required":             []string{"summary", "rules", "unsupported_criteria"},
		"additionalProperties": false,
	}
}

// CopySchema düz içerik şeması: tek seviye obje, yalnızca string alanlar.
// FieldStream parser'ı tam olarak bu şekle dayanır. Uzunluk kuralları
// açıklamada; zorlama content.Validate'te.
//
// properties bilerek struct: Go map'leri JSON'a alfabetik sırayla yazılır
// (email_body, email_subject, sms_text) ve model alanları şemadaki sırayla
// üretir. Struct alan sırası korunur; UI'da ilk kart olan SMS ilk akar.
func CopySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": copyProperties{
			SMSText:      map[string]any{"type": "string", "description": "SMS metni; placeholder'lar render edildikten sonra en fazla 160 karakter."},
			EmailSubject: map[string]any{"type": "string", "description": "Email konusu; tek satır, render sonrası en fazla 80 karakter."},
			EmailBody:    map[string]any{"type": "string", "description": "Düz metin email gövdesi, HTML yok."},
		},
		"required":             []string{"sms_text", "email_subject", "email_body"},
		"additionalProperties": false,
	}
}

type copyProperties struct {
	SMSText      map[string]any `json:"sms_text"`
	EmailSubject map[string]any `json:"email_subject"`
	EmailBody    map[string]any `json:"email_body"`
}
