package pagination

import (
	"encoding/json"
	"testing"
)

func TestNewOffsetPageJSONShape(t *testing.T) {
	page := NewOffsetPage([]string{"a", "b"}, OffsetRequest{Limit: 25, Offset: 0}, 134)
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	want := `{"items":["a","b"],"page":{"limit":25,"offset":0,"total":134}}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

func TestNewOffsetPageNilItemsSerializeAsEmptyArray(t *testing.T) {
	page := NewOffsetPage[string](nil, OffsetRequest{Limit: 10, Offset: 20}, 0)
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	want := `{"items":[],"page":{"limit":10,"offset":20,"total":0}}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

func TestParseOffsetQueryRoundTripIntoPage(t *testing.T) {
	req, err := ParseOffsetQuery(KindAdmin, "", "50")
	if err != nil {
		t.Fatalf("ParseOffsetQuery error = %v", err)
	}
	page := NewOffsetPage([]int{1}, req, 51)
	if page.Page.Limit != 25 || page.Page.Offset != 50 || page.Page.Total != 51 {
		t.Fatalf("Page = %+v, want limit 25, offset 50, total 51", page.Page)
	}
}
