//go:build darwin && cgo

package caller

/*
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stddef.h>
#include <stdint.h>
#include <unistd.h>

typedef struct {
 int duplicate, count, eof, duplicate_cloexec;
 int failed, directory_opened, directory_closed, close_errno;
} n1_fd_result;

// Parse only a canonical bounded numeric descriptor name, with no path input.
static int n1_fd_number(const unsigned char *s, size_t len) {
 if (len == 0 || len > 10 || (len > 1 && s[0] == '0')) return -1;
 unsigned int value = 0;
 for (size_t i = 0; i < len; i++) {
  if (s[i] < '0' || s[i] > '9') return -1;
  unsigned int digit = s[i] - '0';
  if (value > ((unsigned int)INT_MAX - digit) / 10) return -1;
  value = value * 10 + digit;
 }
 return (int)value;
}

static n1_fd_result n1_fd_names(int base, int *out) {
 n1_fd_result r = { .duplicate = -1 };
 errno = 0;
 r.duplicate = fcntl(base, F_DUPFD_CLOEXEC, 4);
 if (r.duplicate < 4 || errno != 0) { r.failed = 1; goto duplicate_cleanup; }
 errno = 0;
 int flags = fcntl(r.duplicate, F_GETFD);
 if (flags < 0 || errno != 0 || !(flags & FD_CLOEXEC)) { r.failed = 1; goto duplicate_cleanup; }
 r.duplicate_cloexec = 1;
 errno = 0;
 DIR *directory = fdopendir(r.duplicate);
 if (directory == NULL) { r.failed = 1; goto duplicate_cleanup; }
 r.directory_opened = 1;
 if (errno != 0) { r.failed = 1; goto directory_cleanup; }
 // Includes at most 65536 numeric entries, two dot entries, and one EOF call.
 for (unsigned int calls = 0; calls < 65539; calls++) {
  errno = 0;
  struct dirent *entry = readdir(directory);
  int read_errno = errno;
  if (entry == NULL) {
   if (read_errno != 0) r.failed = 1;
   else r.eof = 1;
   break;
  }
  if (read_errno != 0) { r.failed = 1; break; }
  size_t len = entry->d_namlen;
  if (len == 0 || len > 10 || entry->d_reclen < offsetof(struct dirent, d_name) + len + 1 || entry->d_name[len] != '\0') {
   r.failed = 1; break;
  }
  if ((len == 1 && entry->d_name[0] == '.') || (len == 2 && entry->d_name[0] == '.' && entry->d_name[1] == '.')) continue;
  int number = n1_fd_number((const unsigned char *)entry->d_name, len);
  if (number < 0 || r.count >= 65536) { r.failed = 1; break; }
  out[r.count++] = number;
 }
 if (!r.eof) r.failed = 1;
 directory_cleanup:
 errno = 0;
 r.directory_closed = closedir(directory) == 0;
 r.close_errno = errno;
 if (!r.directory_closed || r.close_errno != 0) r.failed = 1;
 return r;
 duplicate_cleanup:
 // fdopendir never took ownership here. Even refusal checks the owned close.
 if (r.duplicate >= 0) {
  errno = 0;
  int closed = close(r.duplicate);
  r.close_errno = errno;
  if (closed != 0 || r.close_errno != 0) r.failed = 1;
 }
 return r;
}
*/
import "C"

import "github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"

// Only the already bracketed fixed /dev/fd directory is passed to native code.
// The buffer contains integers only, is fixed-capacity, and is never retained.
func nativeDescriptorNames(base int) ([]uintptr, uintptr, error) {
	values := make([]C.int, maxInheritedDescriptors)
	r := C.n1_fd_names(C.int(base), &values[0])
	proof := descriptorDirectoryProof{int(r.duplicate), int(r.count), r.eof == 1, r.duplicate_cloexec == 1, r.failed != 0, r.directory_opened == 1, r.directory_closed == 1, int(r.close_errno)}
	if !proof.valid(base) {
		return nil, 0, fixed.ErrRefused
	}
	result := make([]uintptr, proof.count)
	for i := range result {
		if values[i] < 0 {
			return nil, 0, fixed.ErrRefused
		}
		result[i] = uintptr(values[i])
	}
	return result, uintptr(proof.duplicate), nil
}

type descriptorDirectoryProof struct {
	duplicate, count                     int
	eof, cloexec, failed, opened, closed bool
	closeErrno                           int
}

func (p descriptorDirectoryProof) valid(base int) bool {
	return base >= 4 && p.duplicate >= 4 && p.duplicate != base && p.count > 0 && p.count <= maxInheritedDescriptors && p.eof && p.cloexec && !p.failed && p.opened && p.closed && p.closeErrno == 0
}

func nativeDescriptorNumber(name []byte) (int, error) {
	if len(name) == 0 || len(name) > 10 {
		return 0, fixed.ErrRefused
	}
	value := C.n1_fd_number((*C.uchar)(&name[0]), C.size_t(len(name)))
	if value < 0 {
		return 0, fixed.ErrRefused
	}
	return int(value), nil
}
