//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package supervisor

import "github.com/weshofmann/boxwarden/internal/networkdiag"

type networkArmRequest struct {
	Version       uint8           `json:"version"`
	Action        string          `json:"action"`
	Binding       Binding         `json:"binding"`
	ExpiresUnixNS uint64          `json:"expires_unix_ns"`
	Arm           networkdiag.Arm `json:"arm"`
}
type networkCollectRequest struct {
	Version       uint8   `json:"version"`
	Action        string  `json:"action"`
	Binding       Binding `json:"binding"`
	ExpiresUnixNS uint64  `json:"expires_unix_ns"`
	OperationID   string  `json:"operation_id"`
}
type networkArmResponse struct {
	Version uint8             `json:"version"`
	Binding Binding           `json:"binding"`
	OK      bool              `json:"ok"`
	Armed   networkdiag.Armed `json:"armed"`
}
type networkCollectResponse struct {
	Version uint8               `json:"version"`
	Binding Binding             `json:"binding"`
	OK      bool                `json:"ok"`
	Summary networkdiag.Summary `json:"summary"`
}
