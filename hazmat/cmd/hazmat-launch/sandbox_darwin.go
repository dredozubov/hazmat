//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#include <sandbox.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Intentional use of deprecated API: sandbox_init() and sandbox_free_error()
// have been deprecated since macOS 10.8. There is no public, non-deprecated
// replacement for applying an SBPL policy string to the current process at
// runtime. Apple's recommended replacement is App Sandbox (entitlements
// declared at build time), which cannot generate per-session policies.
// The private alternatives (sandbox_compile_string, sandbox_apply) are absent
// from all public SDK headers and are worse to depend on. sandbox-exec(1) is
// also deprecated and wraps the same functions. If Apple removes sandbox_init,
// the CGO build will fail with a link error and Hazmat will need a fallback
// backend for signal-correct session launch.
func sandboxInit(policy string) error {
	cPolicy := C.CString(policy)
	defer C.free(unsafe.Pointer(cPolicy))

	var errBuf *C.char
	ret := C.sandbox_init(cPolicy, 0, &errBuf)
	if ret != 0 {
		msg := C.GoString(errBuf)
		C.sandbox_free_error(errBuf)
		return fmt.Errorf("sandbox_init: %s", msg)
	}
	return nil
}
