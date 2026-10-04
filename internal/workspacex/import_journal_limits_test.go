package workspacex

import "testing"

func TestImportJournalAdmitsLargerSourceBounds(t *testing.T) {
	_, base := importJournalFixture(t)
	for _, tc := range []struct {
		name  string
		files int
		total int64
		pass  bool
	}{
		{"larger file list", 4096, 0, true},
		{"larger byte aggregate", 4, 256 << 20, true},
		{"all source bounds", 4096, 256 << 20, true},
		{"too many files", 4097, 0, false},
		{"too many bytes", 4, (256 << 20) + 1, false},
		{"negative bytes", 4, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journal := base
			journal.FileCount, journal.TotalBytes = tc.files, tc.total
			if err := validateImportJournal(journal); (err == nil) != tc.pass {
				t.Fatalf("journal admission = %v, pass=%t", err, tc.pass)
			}
		})
	}
}
