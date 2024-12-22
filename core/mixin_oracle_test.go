package core

import (
	"reflect"
	"testing"
)

func TestSwapResponseView_DecodeTx(t *testing.T) {
	type fields struct {
		Tx string
	}
	tests := []struct {
		name    string
		fields  fields
		want    *SwapTx
		wantErr bool
	}{
		{
			name: "valid tx",
			fields: fields{
				Tx: "mixin://mixin.one/pay/0x1234567890abcdef?asset=BTC&amount=100000000&trace=test-trace&memo=test-memo",
			},
			want: &SwapTx{
				Trace:   "test-trace",
				Payee:   "0x1234567890abcdef",
				Asset:   "BTC",
				Amount:  "100000000",
				Memo:    "test-memo",
				OrderId: "test-memo",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := SwapResponseView{
				Tx: tt.fields.Tx,
			}
			got, err := s.DecodeTx()
			if (err != nil) != tt.wantErr {
				t.Errorf("SwapResponseView.DecodeTx() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SwapResponseView.DecodeTx() = %v, want %v", got, tt.want)
			}
		})
	}
}
