//go:build n1candidate

package hostx

import "testing"

func TestN1CandidateCannotAdmitStockSoftnet(t *testing.T) {
	stock := ToolIdentity{Path: "/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet", Version: "0.19.0", ExecutableSHA256: "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e", ArchiveSHA256: "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"}
	if qualifiedSoftnet(stock) {
		t.Fatal("candidate build admitted unrestricted stock Softnet")
	}
}
