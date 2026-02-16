package xdo

/*
#cgo pkg-config: libxdo

#include <malloc.h>
#include <errno.h>
#include <linux/input.h>
#include <xdo.h>
*/
import "C"
import (
	"runtime"
	"time"
	"unsafe"
)

type Xdo struct {
	p *C.xdo_t
}

func New() (*Xdo, bool) {
	p := C.xdo_new(nil)
	if p == nil {
		return nil, false
	}

	xdo := Xdo{p: p}
	runtime.AddCleanup(&xdo, free, p)
	return &xdo, true
}

func free(p *C.struct_xdo) {
	C.xdo_free(p)
}

func (xdo *Xdo) SendKeysequenceWindowUp(w Window, keys string, delay time.Duration) bool {
	ckeys := C.CString(keys)
	defer C.free(unsafe.Pointer(ckeys))

	return C.xdo_send_keysequence_window_up(xdo.p, C.Window(w), ckeys, C.uint(delay.Seconds())) != 0
}

func (xdo *Xdo) SendKeysequenceWindowDown(w Window, keys string, delay time.Duration) bool {
	ckeys := C.CString(keys)
	defer C.free(unsafe.Pointer(ckeys))

	return C.xdo_send_keysequence_window_down(xdo.p, C.Window(w), ckeys, C.uint(delay.Seconds())) != 0
}

func (xdo *Xdo) MouseDown(w Window, button int) bool {
	return C.xdo_mouse_down(xdo.p, C.Window(w), C.int(button)) != 0
}

func (xdo *Xdo) MouseUp(w Window, button int) bool {
	return C.xdo_mouse_up(xdo.p, C.Window(w), C.int(button)) != 0
}

type Window uint32

const CurrentWindow Window = C.CURRENTWINDOW
