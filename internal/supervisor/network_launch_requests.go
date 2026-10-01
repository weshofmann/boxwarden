//go:build n1diagnostic && !n1candidate

package supervisor

import "github.com/weshofmann/boxwarden/internal/networkdiag"

type networkObserveResponse struct {
	Version     uint8                         `json:"version"`
	Binding     Binding                       `json:"binding"`
	OK          bool                          `json:"ok"`
	Observation networkdiag.LaunchObservation `json:"observation"`
}
