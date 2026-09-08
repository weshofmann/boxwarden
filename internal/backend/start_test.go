package backend

import "testing"

func TestValidateStartRequestRejectsNonCanonicalPaths(t *testing.T) {
	valid := StartRequest{
		ObjectID:            "boxwarden-work-dev",
		SerialDevice:        "/dev/ttys004",
		GenerationDirectory: "/private/runtime/work/dev/generation-1",
	}
	for name, request := range map[string]StartRequest{
		"relative serial device":         {ObjectID: valid.ObjectID, SerialDevice: "dev/ttys004", GenerationDirectory: valid.GenerationDirectory},
		"serial device traversal":        {ObjectID: valid.ObjectID, SerialDevice: "/dev/../ttys004", GenerationDirectory: valid.GenerationDirectory},
		"root serial device":             {ObjectID: valid.ObjectID, SerialDevice: "/", GenerationDirectory: valid.GenerationDirectory},
		"relative generation directory":  {ObjectID: valid.ObjectID, SerialDevice: valid.SerialDevice, GenerationDirectory: "runtime/generation-1"},
		"generation directory traversal": {ObjectID: valid.ObjectID, SerialDevice: valid.SerialDevice, GenerationDirectory: "/private/runtime/../generation-1"},
		"root generation directory":      {ObjectID: valid.ObjectID, SerialDevice: valid.SerialDevice, GenerationDirectory: "/"},
		"unsafe object identifier":       {ObjectID: "--unsafe", SerialDevice: valid.SerialDevice, GenerationDirectory: valid.GenerationDirectory},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateStartRequest(request); err == nil {
				t.Fatalf("ValidateStartRequest(%#v) error = nil, want refusal", request)
			}
		})
	}

	if err := ValidateStartRequest(valid); err != nil {
		t.Fatalf("ValidateStartRequest(%#v) error = %v", valid, err)
	}
}
