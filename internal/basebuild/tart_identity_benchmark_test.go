package basebuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Manual, read-only host benchmark. It never runs in ordinary test suites.
func BenchmarkTartBundleIdentity(b *testing.B) {
	home, id := os.Getenv("BOXWARDEN_BENCH_TART_HOME"), os.Getenv("BOXWARDEN_BENCH_CANDIDATE")
	if home == "" || id == "" {
		b.Skip("set BOXWARDEN_BENCH_TART_HOME and BOXWARDEN_BENCH_CANDIDATE for a stopped bundle")
	}
	disk, err := os.Stat(filepath.Join(home, "vms", id, "disk.img"))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(disk.Size())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fingerprintTartBundle(context.Background(), home, id); err != nil {
			b.Fatal(err)
		}
	}
}
