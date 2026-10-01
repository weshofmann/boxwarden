package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const MaxReceiptBytes = 16384
const StockConfigSHA = "0fe963334098b05399a6e31d4df8de5759734e5c971c4ba70e2ccbc3aac2020b"
const CandidateConfigSHA = "3ca4aaa4f4ebed8475d186679a08008c82db3fe737e48feda944c632d09e109c"
const SoftnetSHA = "1bb12bec8821835ada8c036426c2c03061be6cebdf4a1b658249d268fc8bde05"

type Window struct {
	LockSHA           string `json:"lock_sha"`
	ID                string `json:"id"`
	StartedUnixNS     uint64 `json:"started_unix_ns"`
	ExpiresUnixNS     uint64 `json:"expires_unix_ns"`
	ContinuousStartNS uint64 `json:"continuous_start_ns"`
	ContinuousLimitNS uint64 `json:"continuous_limit_ns"`
}
type Peer struct {
	HostPinSHA string `json:"host_pin_sha"`
	Role       string `json:"role"`
	Session    string `json:"session"`
	Generation string `json:"generation"`
	Backend    string `json:"backend"`
	Reaped     bool   `json:"reaped"`
	Deleted    bool   `json:"deleted"`
}
type Attempt struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Closed  bool   `json:"closed"`
}
type PublicRecord struct {
	Name     string `json:"name"`
	SHA      string `json:"sha"`
	WindowID string `json:"window_id"`
}

const BaseName = "boxwarden-alpha-base-af8cf2494da4e5f578f651fbd12bbd87"

var PublicRecordNames = [2]string{"identity/ssh-user-ca/metadata.json", "goldens/records/" + BaseName + ".json"}

type Handoff struct {
	Budget         Budget          `json:"budget"`
	PublicRecords  [2]PublicRecord `json:"public_records"`
	Version        int             `json:"version"`
	Window         Window          `json:"window"`
	Configs        [2]string       `json:"configs"`
	Pair           [2]Peer         `json:"pair"`
	Attempts       []Attempt       `json:"attempts"`
	ArchiveSHA     string          `json:"archive_sha"`
	ArchiveBytes   uint64          `json:"archive_bytes"`
	ArchiveFiles   uint16          `json:"archive_files"`
	DispatchClosed bool            `json:"dispatch_closed"`
	RuntimeClean   bool            `json:"runtime_clean"`
}
type Completion struct {
	Version          int    `json:"version"`
	Window           Window `json:"window"`
	HandoffSHA       string `json:"handoff_sha"`
	ArchiveSHA       string `json:"archive_sha"`
	SoftnetSHA       string `json:"softnet_sha"`
	Removed          uint8  `json:"removed"`
	DirectoryRemoved bool   `json:"directory_removed"`
	ParentSynced     bool   `json:"parent_synced"`
	HandlesClosed    bool   `json:"handles_closed"`
}

var ErrRefused = errors.New("n1 contract refused")

const MaxLockBytes = 65536
const WindowNS uint64 = 30 * 60 * 1000000000
const PlannedExit = 20
const PackageRoot = "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package"
const EvidenceRoot = "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/live-window-1"
const StateRoot = "/Volumes/BoxwardenAlphaQualification/n1-diagnostic-20260930-state"
const VolumeUUID = "a178510a-d5ec-4495-828b-bd5445e2b66d"
const ConfigRoot = PackageRoot + "/config"
const HandoffName = "precleanup-handoff.json"
const CompletionName = "cleanup-completion.json"
const LockPath = PackageRoot + "/static-lock.json"
const MaxWitnessBytes = 512

var artifactNames = [6]string{"n1-window", "n1-run-window", "n1-attend", "n1-attend-root", "n1-cleanup", "n1-closeout"}

type Artifact struct {
	Name   string `json:"name"`
	SHA    string `json:"sha"`
	Source string `json:"source"`
}
type SystemImage struct {
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	SHA              string `json:"sha"`
	QualificationSHA string `json:"qualification_sha"`
}
type StaticLock struct {
	Version           int           `json:"version"`
	ProtectedSudo     ProtectedSudo `json:"protected_sudo"`
	Files             []StaticFile  `json:"files"`
	Artifacts         [6]Artifact   `json:"artifacts"`
	Configs           [2]string     `json:"configs"`
	SoftnetSHA        string        `json:"softnet_sha"`
	CatalogueSHA      string        `json:"catalogue_sha"`
	SchemaSHA         string        `json:"schema_sha"`
	ProcedureSHA      string        `json:"procedure_sha"`
	SystemImages      []SystemImage `json:"system_images"`
	NoReplacement     bool          `json:"no_replacement"`
	ActiveSeconds     uint16        `json:"active_seconds"`
	AttendanceSeconds uint16        `json:"attendance_seconds"`
}

// Static hashes are supplied only after complete source graphs and twin builds.
// A lock is data, never command/path or admission authority by itself.
func ArtifactPath(index int) string {
	if index < 0 || index >= len(artifactNames) {
		return ""
	}
	return PackageRoot + "/artifacts/" + artifactNames[index]
}
func (s StaticLock) Valid() bool {
	if s.Version != 2 || !s.ProtectedSudo.Valid() || !validStaticFiles(s.Files) || s.Configs != [2]string{StockConfigSHA, CandidateConfigSHA} || s.SoftnetSHA != SoftnetSHA || !digest(s.CatalogueSHA) || !digest(s.SchemaSHA) || !digest(s.ProcedureSHA) || s.CatalogueSHA != s.Files[30].SHA || s.SchemaSHA != s.Files[31].SHA || s.ProcedureSHA != s.Files[28].SHA || !s.NoReplacement || s.ActiveSeconds != 1200 || s.AttendanceSeconds != 600 || len(s.SystemImages) != len(SystemPaths) {
		return false
	}
	seen := map[string]bool{}
	for i, a := range s.Artifacts {
		if a.Name != artifactNames[i] || !digest(a.SHA) || !lowerHex(a.Source, 40) || seen[a.SHA] {
			return false
		}
		seen[a.SHA] = true
	}
	for i, x := range s.SystemImages {
		if x.Kind != "digest" || x.Path != SystemPaths[i] || !digest(x.SHA) || !digest(x.QualificationSHA) || seen[x.SHA] {
			return false
		}
		seen[x.SHA] = true
	}
	return true
}
func ParseStaticLock(raw []byte) (StaticLock, error) {
	var s StaticLock
	if strict(raw, MaxLockBytes, &s) != nil || !s.Valid() {
		return StaticLock{}, ErrRefused
	}
	return s, nil
}
func lowerHex(s string, n int) bool {
	if len(s) != n || strings.Trim(s, "0") == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func digest(s string) bool { return lowerHex(s, 64) }
func UUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	return lowerHex(strings.ReplaceAll(s, "-", ""), 32) && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
func (w Window) Valid() bool {
	return digest(w.LockSHA) && UUID(w.ID) && w.StartedUnixNS > 0 && w.ExpiresUnixNS <= 9223372036854775807 && w.ContinuousStartNS > 0 && w.ExpiresUnixNS > w.StartedUnixNS && w.ContinuousLimitNS > w.ContinuousStartNS && w.ExpiresUnixNS-w.StartedUnixNS == WindowNS && w.ContinuousLimitNS-w.ContinuousStartNS == WindowNS
}
func (w Window) Check(wall, continuous uint64) error {
	if !w.Valid() || wall < w.StartedUnixNS || wall >= w.ExpiresUnixNS || continuous < w.ContinuousStartNS || continuous >= w.ContinuousLimitNS {
		return ErrRefused
	}
	return nil
}
func (h Handoff) Valid() bool {
	if h.Version != 1 || !h.Window.Valid() || !h.Budget.Valid(h.Window) || h.Configs != [2]string{StockConfigSHA, CandidateConfigSHA} || !digest(h.ArchiveSHA) || h.ArchiveBytes == 0 || h.ArchiveBytes > 2<<20 || h.ArchiveFiles == 0 || h.ArchiveFiles > 128 || !h.DispatchClosed || !h.RuntimeClean || len(h.Attempts) == 0 || len(h.Attempts) > 32 {
		return false
	}
	for i, p := range h.PublicRecords {
		if p.Name != PublicRecordNames[i] || !digest(p.SHA) || p.WindowID != h.Window.ID {
			return false
		}
	}
	seen := map[string]bool{}
	for i, p := range h.Pair {
		role := "control"
		if i == 1 {
			role = "candidate"
		}
		if !digest(p.HostPinSHA) || p.Role != role || !UUID(p.Session) || !UUID(p.Generation) || !p.Reaped || !p.Deleted || len(p.Backend) == 0 || len(p.Backend) > 127 || seen[p.Session] || seen[p.Generation] || seen[p.Backend] {
			return false
		}
		for _, c := range p.Backend {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
		seen[p.Session] = true
		seen[p.Generation] = true
		seen[p.Backend] = true
	}
	commands := map[string]bool{}
	for _, a := range h.Attempts {
		if !UUID(a.ID) || seen[a.ID] || !a.Closed || !commandID(a.Command) || commands[a.Command] {
			return false
		}
		seen[a.ID] = true
		commands[a.Command] = true
	}
	return true
}
func commandID(s string) bool {
	switch s {
	case "enroll", "install", "control-create", "candidate-create", "control-stage", "candidate-stage", "control-restart", "candidate-restart", "initial-review", "final-review", "control-copy", "control-read", "candidate-copy", "candidate-read", "network-controls", "observer-control", "observer-candidate", "arm", "connect", "collect", "control-stop", "candidate-stop", "control-delete", "candidate-delete", "archive":
		return true
	}
	return false
}
func ParseHandoff(raw []byte) (Handoff, error) {
	var h Handoff
	if strict(raw, MaxReceiptBytes, &h) != nil || !h.Valid() {
		return Handoff{}, ErrRefused
	}
	return h, nil
}
func (c Completion) Valid() bool {
	return c.Version == 1 && c.Window.Valid() && digest(c.HandoffSHA) && digest(c.ArchiveSHA) && c.SoftnetSHA == SoftnetSHA && c.Removed == 3 && c.DirectoryRemoved && c.ParentSynced && c.HandlesClosed
}
func ParseCompletion(raw []byte) (Completion, error) {
	var c Completion
	if strict(raw, MaxReceiptBytes, &c) != nil || !c.Valid() {
		return Completion{}, ErrRefused
	}
	return c, nil
}
func ValidateCompletion(c Completion, h Handoff, handoffSHA string, exit int, closed bool, wall, mono uint64) error {
	if !c.Valid() || !h.Valid() || exit != 0 || !closed || c.Window != h.Window || c.HandoffSHA != handoffSHA || c.ArchiveSHA != h.ArchiveSHA {
		return ErrRefused
	}
	return h.Check(wall, mono)
}

// A terminal witness records actual acknowledged return, not namespace effect.
// Phase 1 crosses L exec U; phase 2 is H publication; phase 3 is R direct Wait.
type Witness struct {
	Version       int    `json:"v"`
	Phase         uint8  `json:"phase"`
	LockSHA       string `json:"lock"`
	WindowID      string `json:"window"`
	HandoffSHA    string `json:"handoff"`
	CompletionSHA string `json:"completion"`
	Exit          int    `json:"exit"`
}

func (w Witness) Valid() bool {
	return w.Version == 1 && w.Phase >= 1 && w.Phase <= 3 && digest(w.LockSHA) && UUID(w.WindowID) && digest(w.HandoffSHA) && ((w.Phase == 1 && w.CompletionSHA == "" && w.Exit == PlannedExit) || (w.Phase != 1 && digest(w.CompletionSHA) && w.Exit == 0))
}
func ParseWitness(raw []byte) (Witness, error) {
	var w Witness
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || strict(raw[:len(raw)-1], MaxWitnessBytes-1, &w) != nil || !w.Valid() {
		return Witness{}, ErrRefused
	}
	return w, nil
}
func Encode(v any) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > MaxReceiptBytes {
		return nil, ErrRefused
	}
	return raw, nil
}
func EncodeWitness(w Witness) ([]byte, error) {
	if !w.Valid() {
		return nil, ErrRefused
	}
	raw, e := Encode(w)
	if e != nil || len(raw)+1 > MaxWitnessBytes {
		return nil, ErrRefused
	}
	return append(raw, '\n'), nil
}
func SHA(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }

// Preflight before struct decode bounds depth/tokens/strings and duplicates.
// Canonical byte equality rejects missing/null scalars and alternative forms.
func strict(raw []byte, limit int, dst any) error {
	if len(raw) == 0 || len(raw) > limit {
		return ErrRefused
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var walk func(int) error
	walk = func(depth int) error {
		nodes++
		if depth > 8 || nodes > 4096 {
			return ErrRefused
		}
		t, e := d.Token()
		if e != nil {
			return ErrRefused
		}
		switch x := t.(type) {
		case string:
			if len(x) > 512 {
				return ErrRefused
			}
		case json.Number:
			if len(x) > 20 {
				return ErrRefused
			}
		case json.Delim:
			if x != '{' && x != '[' {
				return ErrRefused
			}
			seen := map[string]bool{}
			count := 0
			for d.More() {
				count++
				if count > 256 {
					return ErrRefused
				}
				if x == '{' {
					k, e := d.Token()
					s, ok := k.(string)
					if e != nil || !ok || len(s) > 64 || seen[s] {
						return ErrRefused
					}
					seen[s] = true
				}
				if walk(depth+1) != nil {
					return ErrRefused
				}
			}
			end, e := d.Token()
			if e != nil || x == '{' && end != json.Delim('}') || x == '[' && end != json.Delim(']') {
				return ErrRefused
			}
		}
		return nil
	}
	if walk(0) != nil {
		return ErrRefused
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrRefused
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return ErrRefused
	}
	encoded, e := json.Marshal(dst)
	if e != nil || !bytes.Equal(raw, encoded) {
		return ErrRefused
	}
	return nil
}

func ParseOrigin(raw []byte) (Window, error) {
	var w Window
	if len(raw) == 0 || len(raw) > MaxWitnessBytes || raw[len(raw)-1] != '\n' || strict(raw[:len(raw)-1], MaxWitnessBytes-1, &w) != nil || !w.Valid() {
		return Window{}, ErrRefused
	}
	return w, nil
}
