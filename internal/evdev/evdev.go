package evdev

/*
#include <linux/input.h>

static inline unsigned int wrap_EVIOCGNAME(int len) {
	return EVIOCGNAME(len);
}

static inline unsigned int wrap_EVIOCGBIT(int min, int max) {
	return EVIOCGBIT(min, max);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const longBits = unsafe.Sizeof(C.long(0))

type Device struct {
	file *os.File

	Name                              string
	BusType, Vendor, Product, Version uint16

	bits                                                                 []byte
	bitsREL, bitsABS, bitsLED, bitsKEY, bitsSW, bitsMSC, bitsFF, bitsSND []byte
}

func Open(path string) (*Device, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	d := Device{file: file}
	return &d, d.init()
}

func (d *Device) init() error {
	conn, err := d.file.SyscallConn()
	if err != nil {
		return err
	}

	var buf [256]C.char
	err = cctl(conn, uintptr(C.wrap_EVIOCGNAME(256)), &buf[0])
	if err != nil {
		return fmt.Errorf("get device name: %w", err)
	}
	d.Name = C.GoString(&buf[0])

	var id C.struct_input_id
	err = cctl(conn, C.EVIOCGID, &id)
	if err != nil {
		return fmt.Errorf("get device info: %w", err)
	}
	d.BusType = uint16(id.bustype)
	d.Vendor = uint16(id.vendor)
	d.Product = uint16(id.product)
	d.Version = uint16(id.version)

	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(0, 0x1F)), &buf[0])
	if err != nil {
		return fmt.Errorf("get device capabilities: %w", err)
	}
	d.bits = unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), 0x1F)

	var bitsREL [(C.REL_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_REL, C.int(len(bitsREL)))), &bitsREL[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsREL = bitsREL[:]

	var bitsABS [(C.ABS_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_ABS, C.int(len(bitsABS)))), &bitsABS[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsABS = bitsABS[:]

	var bitsLED [(C.LED_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_LED, C.int(len(bitsLED)))), &bitsLED[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsLED = bitsLED[:]

	var bitsKEY [(C.KEY_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_KEY, C.int(len(bitsKEY)))), &bitsKEY[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsKEY = bitsKEY[:]

	var bitsSW [(C.SW_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_SW, C.int(len(bitsSW)))), &bitsSW[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsSW = bitsSW[:]

	var bitsMSC [(C.MSC_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_MSC, C.int(len(bitsMSC)))), &bitsMSC[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsMSC = bitsMSC[:]

	var bitsFF [(C.FF_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_FF, C.int(len(bitsFF)))), &bitsFF[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsFF = bitsFF[:]

	var bitsSND [(C.SND_CNT + longBits - 1) / 8]byte
	err = cctl(conn, uintptr(C.wrap_EVIOCGBIT(C.EV_SND, C.int(len(bitsSND)))), &bitsSND[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsSND = bitsSND[:]

	return nil
}

func (d *Device) Close() error {
	return d.file.Close()
}

func (d *Device) typeCodes(t uint16) []byte {
	switch t {
	case C.EV_KEY:
		return d.bitsKEY
	case C.EV_REL:
		return d.bitsREL
	case C.EV_ABS:
		return d.bitsABS
	case C.EV_MSC:
		return d.bitsMSC
	case C.EV_SW:
		return d.bitsSW
	case C.EV_LED:
		return d.bitsLED
	case C.EV_SND:
		return d.bitsSND
	case C.EV_FF:
		return d.bitsFF
	default:
		return nil
	}
}

func (d *Device) HasEventType(t uint16) bool {
	return isBitSet(d.bits, t)
}

func (d *Device) HasEventCode(t, code uint16) bool {
	return d.HasEventType(t) && isBitSet(d.typeCodes(t), code)
}

func (d *Device) NextEvent() (InputEvent, error) {
	var ev [unsafe.Sizeof(C.struct_input_event{})]byte
	_, err := io.ReadFull(d.file, ev[:])
	if err != nil {
		return InputEvent{}, fmt.Errorf("read: %w", err)
	}

	c := *(*C.struct_input_event)(unsafe.Pointer(&ev[0]))
	return InputEvent{
		Type:  uint16(c._type),
		Code:  uint16(c.code),
		Value: int32(c.value),
	}, nil
}

type InputEvent struct {
	Type  uint16
	Code  uint16
	Value int32
}

func (ev InputEvent) Is(t, code uint16) bool {
	return (ev.Type == t) && (ev.Code == code)
}

func control(conn syscall.RawConn, f func(uintptr) error) error {
	var ferr error
	err := conn.Control(func(fd uintptr) { ferr = f(fd) })
	return errors.Join(err, ferr)
}

func ioctl[T any](fd, name uintptr, data *T) unix.Errno {
	_, _, err := unix.Syscall(unix.SYS_IOCTL, fd, name, uintptr(unsafe.Pointer(data)))
	return err
}

func cctl[T any](conn syscall.RawConn, name uintptr, data *T) error {
	return control(conn, func(fd uintptr) error {
		return fromErrno(ioctl(fd, name, data))
	})
}

func fromErrno(err unix.Errno) error {
	if err == 0 {
		return nil
	}
	return err
}

func isBitSet(bits []byte, bit uint16) bool {
	return bits[bit/8]&(1<<(bit%8)) != 0
}
