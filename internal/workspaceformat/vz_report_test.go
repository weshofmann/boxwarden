package workspaceformat

import (
	"strings"
	"testing"
)

const reportTransaction = "0123456789abcdef0123456789abcdef"
const reportUUID = "12345678-1234-1234-1234-123456789abc"

const goodVZReport = `{"version":1,"transaction":"` + reportTransaction + `","vm_stopped":true,"runtime_network_devices":0,"storage_devices":1,"storage_read_only":false,"serial_ports":2,"socket_devices":0,"shared_directory_devices":0,"observed_uuid":"` + reportUUID + `","whole_device":true,"filesystem_clean":true}`

func TestParseVZRunEvidenceRequiresExactStoppedIsolatedReport(t *testing.T) {
	evidence, err := parseVZRunEvidence([]byte(goodVZReport+"\n"), reportTransaction, reportUUID)
	if err != nil || evidence != (FormatEvidence{ObservedUUID: reportUUID, WholeDevice: true, FilesystemClean: true}) {
		t.Fatalf("rejected exact VZ result: evidence=%+v err=%v", evidence, err)
	}
	for _, malformed := range []string{
		strings.Replace(goodVZReport, `"vm_stopped":true`, `"vm_stopped":false`, 1),
		strings.Replace(goodVZReport, `"runtime_network_devices":0`, `"runtime_network_devices":1`, 1),
		strings.Replace(goodVZReport, `"storage_read_only":false`, `"storage_read_only":true`, 1),
		strings.Replace(goodVZReport, `"transaction":"`+reportTransaction+`"`, `"transaction":"`+strings.Repeat("0", 32)+`"`, 1),
		strings.Replace(goodVZReport, `"filesystem_clean":true`, `"filesystem_clean":false`, 1),
		strings.Replace(goodVZReport, `"version":1`, `"version":2`, 1),
		goodVZReport[:len(goodVZReport)-1] + `,"vm_stopped":true}`,
		goodVZReport + goodVZReport,
	} {
		if _, err := parseVZRunEvidence([]byte(malformed), reportTransaction, reportUUID); err == nil {
			t.Fatalf("accepted malformed VZ result: %s", malformed)
		}
	}
}
