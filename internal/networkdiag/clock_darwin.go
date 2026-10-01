//go:build n1diagnostic && !n1candidate && darwin && cgo

package networkdiag

/*
#include <mach/mach_time.h>
#include <stdint.h>
static int bw_diag_continuous(uint64_t *out){mach_timebase_info_data_t t;if(mach_timebase_info(&t)!=KERN_SUCCESS||!t.denom)return 1;__uint128_t n=(__uint128_t)mach_continuous_time()*t.numer/t.denom;if(n>UINT64_MAX)return 1;*out=(uint64_t)n;return 0;}
*/
import "C"
import "time"

func nativeClock() (ClockReading, error) {
	var n C.uint64_t
	if C.bw_diag_continuous(&n) != 0 {
		return ClockReading{}, ErrMetadata
	}
	wall := time.Now().UnixNano()
	if wall <= 0 || n == 0 {
		return ClockReading{}, ErrMetadata
	}
	return ClockReading{uint64(wall), uint64(n)}, nil
}
