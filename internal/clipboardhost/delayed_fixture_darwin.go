//go:build darwin && cgo

package clipboardhost

/*
#include <stdlib.h>
void test_seed_promised(const char *);
*/
import "C"
import (
	"strings"
	"unsafe"
)

func seedPromised(name string) {
	if !strings.HasPrefix(name, "org.boxwarden.test.") {
		panic("invalid private pasteboard fixture")
	}
	c := C.CString(name)
	defer C.free(unsafe.Pointer(c))
	C.test_seed_promised(c)
}
