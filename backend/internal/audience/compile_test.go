package audience

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func TestCompileExample(t *testing.T) {
	p := Plan{Summary: "x", Rules: []Rule{{FieldCity, OpEq, "istanbul"}, {FieldCartAddedWithinDays, OpLte, 30}, {FieldPurchasedSinceLastCart, OpEq, false}}}
	where, args, err := Compile(p, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "c.city_key = $1 AND c.last_cart_at >= $2::timestamptz - make_interval(days => $3::int) AND " +
		"(c.last_cart_at IS NOT NULL AND (c.last_purchase_at IS NULL OR c.last_purchase_at < c.last_cart_at))"
	if where != want {
		t.Fatalf("where:\n%s\nwant:\n%s", where, want)
	}
	if !reflect.DeepEqual(args, []any{"istanbul", now, 30}) {
		t.Fatalf("args %v", args)
	}
}

// Değerler SQL metnine asla girmez; now tek parametre olarak paylaşılır.
func TestCompileValuesNeverInSQL(t *testing.T) {
	p := Plan{Rules: []Rule{
		{FieldCity, OpIn, []string{"ankara", "izmir"}},
		{FieldTotalOrders, OpGte, 7},
		{FieldPurchasedWithinDays, OpGt, 31},
		{FieldPurchasedWithinDays, OpLte, 97},
		{FieldCartAddedWithinDays, OpLte, 13},
	}}
	where, args, err := Compile(p, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"ankara", "izmir", "7", "31", "97", "13"} {
		if strings.Contains(where, v) {
			t.Fatalf("value %q leaked into SQL: %s", v, where)
		}
	}
	nowIdx := -1
	for i, a := range args {
		if a == now {
			if nowIdx != -1 {
				t.Fatalf("now passed twice: %v", args)
			}
			nowIdx = i
		}
	}
	if ref := fmt.Sprintf("$%d::timestamptz", nowIdx+1); nowIdx == -1 || strings.Count(where, ref) != 3 {
		t.Fatalf("now should be one parameter referenced by all 3 time rules: %v / %s", args, where)
	}
}

func TestCompileRejectsEmptyPlan(t *testing.T) {
	if _, _, err := Compile(Plan{}, now); err == nil {
		t.Fatal("expected error")
	}
}

func TestChannelFilter(t *testing.T) {
	if s, _ := ChannelFilter(ChannelSMS); s != "c.sms_opt_in" {
		t.Fatal(s)
	}
	if _, err := ChannelFilter("push"); err == nil {
		t.Fatal("unknown channel must error")
	}
}
