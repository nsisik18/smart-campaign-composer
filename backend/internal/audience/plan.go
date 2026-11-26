// Package audience Claude'un ürettiği segment planını (SegmentPlan) tipler,
// doğrular ve parametreli SQL'e derler.
//
// Güvenlik sınırı burasıdır, prompt değil: Claude'dan gelen hiçbir string
// SQL metnine girmez. Alan/operatör allowlist'i, her alan için sabit SQL
// parçası ve parametre olarak geçen değerler.
package audience

// Field ve Op kanonik değerleri. Claude'dan gelen enum'lar büyük/küçük harf
// farklı dönebilir; Validate karşılaştırmadan önce normalize eder.
type Field string

const (
	FieldCity                   Field = "city"
	FieldCartAddedWithinDays    Field = "cart_added_within_days"
	FieldPurchasedSinceLastCart Field = "purchased_since_last_cart"
	FieldPurchasedWithinDays    Field = "purchased_within_days"
	FieldTotalOrders            Field = "total_orders"
)

type Op string

const (
	OpEq  Op = "eq"
	OpIn  Op = "in"
	OpLte Op = "lte"
	OpGte Op = "gte"
	OpGt  Op = "gt"
)

const (
	MaxRules       = 10
	MaxUnsupported = 10
	MaxSummary     = 300
	MaxCriterion   = 200
)

// Rule doğrulanmış ve normalize edilmiş tek kuraldır. Value'nun dinamik
// tipi alana göre sabittir: city/eq -> string, city/in -> []string,
// gün ve sipariş alanları -> int, purchased_since_last_cart -> bool.
type Rule struct {
	Field Field `json:"field"`
	Op    Op    `json:"op"`
	Value any   `json:"value"`
}

// Plan doğrulanmış segmenttir; DB'ye (campaigns.segment) bu haliyle yazılır.
// Kurallar AND ile birleşir.
type Plan struct {
	Summary             string   `json:"summary"`
	Rules               []Rule   `json:"rules"`
	UnsupportedCriteria []string `json:"unsupported_criteria"`
}

// Issue makine tarafından üretilmiş, alan bazlı doğrulama hatasıdır.
// Message hem UI'da gösterilir hem onarım çağrısında Claude'a gönderilir.
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Path + ": " + i.Message }
