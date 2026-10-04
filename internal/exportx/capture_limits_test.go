package exportx

import "testing"

type inspectorCountingSink struct{ count int64 }

func (w *inspectorCountingSink) Write(data []byte) (int, error) {
	w.count += int64(len(data))
	return len(data), nil
}

func TestDefaultInspectorSpoolDrainsBeyond640MiBWithoutWriting(t *testing.T) {
	sink := &inspectorCountingSink{}
	writer := &boundedInspectorWriter{output: sink, limit: maxInspectorStreamBytes}
	chunk := make([]byte, 1<<20)
	// Exercise the production writer at its actual default, without a 640 MiB
	// allocation or disposable disk write. Child/EOF binding has separate tests.
	for i := 0; i < 640; i++ {
		if n, err := writer.Write(chunk); err != nil || n != len(chunk) {
			t.Fatalf("bounded write %d=%d, %v", i, n, err)
		}
	}
	if writer.overflow || sink.count != 640<<20 {
		t.Fatalf("bounded stream rejected: bytes=%d overflow=%t", sink.count, writer.overflow)
	}
	if n, err := writer.Write([]byte{1}); err != nil || n != 1 || !writer.overflow || sink.count != 640<<20 {
		t.Fatalf("overflow escaped spool bound: bytes=%d n=%d err=%v", sink.count, n, err)
	}
	if n, err := writer.Write(chunk); err != nil || n != len(chunk) || sink.count != 640<<20 {
		t.Fatalf("overflow was not drained: bytes=%d n=%d err=%v", sink.count, n, err)
	}
}
