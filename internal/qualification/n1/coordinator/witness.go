//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"encoding/binary"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

type phaseEnvelope struct {
	Started clock.Reading `json:"started"`
	Closed  clock.Reading `json:"closed"`
}
type argumentWitness struct {
	Bytes uint32 `json:"bytes"`
	SHA   string `json:"sha"`
}
type commandWitness struct {
	Argc               int               `json:"argc"`
	Arguments          []argumentWitness `json:"arguments"`
	LengthDelimitedSHA string            `json:"length_delimited_sha"`
}

func argvWitness(path string, args []string) commandWitness {
	v := append([]string{path}, args...)
	r := commandWitness{Argc: len(v), Arguments: make([]argumentWitness, len(v))}
	var joined []byte
	for i, a := range v {
		r.Arguments[i] = argumentWitness{uint32(len(a)), contract.SHA([]byte(a))}
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(a)))
		joined = append(joined, n[:]...)
		joined = append(joined, []byte(a)...)
	}
	r.LengthDelimitedSHA = contract.SHA(joined)
	return r
}
func (e *engine) witness(path string, args []string) {
	e.commands = append(e.commands, argvWitness(path, args))
}
