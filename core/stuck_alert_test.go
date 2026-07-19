package core

import (
	"math/big"
	"strings"
	"testing"
)

func TestFmtAlertAmount(t *testing.T) {
	for _, tc := range []struct {
		cur  string
		v    int64
		want string
	}{
		{TonSymbol, 1_500_000_000, "1.5 TON"},
		{"USDT", 1_000_000, "1 USDT"},
		{"UNKNOWN", 42, "42 UNKNOWN (raw)"},
	} {
		if got := fmtAlertAmount(tc.cur, big.NewInt(tc.v)); got != tc.want {
			t.Errorf("fmtAlertAmount(%s, %d) = %q, want %q", tc.cur, tc.v, got, tc.want)
		}
	}
}

func TestRenderStuckAlert(t *testing.T) {
	stuck := []stuckWithdrawal{
		{QueryID: 7, Currency: TonSymbol, Need: big.NewInt(1_000_000_000), Have: big.NewInt(400_000_000)},
		// jetton balance covers the amount => the real cause is TON gas
		{QueryID: 8, Currency: "USDT", Need: big.NewInt(1_000_000), Have: big.NewInt(2_000_000)},
	}
	got := renderStuckAlert(stuck, "SWEEPS\n")

	for _, want := range []string{
		"query_id 7: нужно 1 TON, на hot 0.4 TON",
		"query_id 8: нужно 1 USDT, на hot 2 USDT (не хватает TON на газ)",
		"SWEEPS\n",
		"пополните горячий кошелёк",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("alert text missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(strings.Split(got, "\n")[1], "газ") {
		t.Errorf("gas note must not appear on the TON line:\n%s", got)
	}
}
