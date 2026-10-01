//go:build darwin && cgo

package clock

/*
#include <mach/mach_time.h>
#include <stdint.h>
static int bw_continuous(uint64_t *out){mach_timebase_info_data_t t;if(mach_timebase_info(&t)!=KERN_SUCCESS||!t.denom)return 1;__uint128_t value=(__uint128_t)mach_continuous_time()*t.numer/t.denom;if(value>UINT64_MAX)return 1;*out=(uint64_t)value;return 0;}
*/
import "C"
import "time"

func Now() (Reading, error) {
	var n C.uint64_t
	if C.bw_continuous(&n) != 0 {
		return Reading{}, ErrClock
	}
	w := time.Now().UnixNano()
	if w <= 0 {
		return Reading{}, ErrClock
	}
	return Reading{uint64(w), uint64(n)}, nil
}
