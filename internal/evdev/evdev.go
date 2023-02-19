package evdev

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Device struct {
	file *os.File

	Name string
	ID   InputID

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

	var buf [256]byte
	err = cctl(conn, eviocgname(uintptr(len(buf))), &buf[0])
	if err != nil {
		return fmt.Errorf("get device name: %w", err)
	}
	d.Name = fromNTString(buf[:])

	err = cctl(conn, eviocgid, &d.ID)
	if err != nil {
		return fmt.Errorf("get device info: %w", err)
	}

	var bits [0x1F]byte
	err = cctl(conn, eviocgbit(0, uintptr(len(bits))), &bits[0])
	if err != nil {
		return fmt.Errorf("get device capabilities: %w", err)
	}
	d.bits = bits[:]

	var bitsREL [(relCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evRel, uintptr(len(bitsREL)))), &bitsREL[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsREL = bitsREL[:]

	var bitsABS [(absCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evAbs, uintptr(len(bitsABS)))), &bitsABS[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsABS = bitsABS[:]

	var bitsLED [(ledCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evLed, uintptr(len(bitsLED)))), &bitsLED[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsLED = bitsLED[:]

	var bitsKEY [(keyCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evKey, uintptr(len(bitsKEY)))), &bitsKEY[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsKEY = bitsKEY[:]

	var bitsSW [(swCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evSw, uintptr(len(bitsSW)))), &bitsSW[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsSW = bitsSW[:]

	var bitsMSC [(mscCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evMsc, uintptr(len(bitsMSC)))), &bitsMSC[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsMSC = bitsMSC[:]

	var bitsFF [(ffCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evFf, uintptr(len(bitsFF)))), &bitsFF[0])
	if err != nil {
		return fmt.Errorf("get type bits: %w", err)
	}
	d.bitsFF = bitsFF[:]

	var bitsSND [(sndCount + wordbits - 1) / 8]byte
	err = cctl(conn, uintptr(eviocgbit(evSnd, uintptr(len(bitsSND)))), &bitsSND[0])
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
	case evKey:
		return d.bitsKEY
	case evRel:
		return d.bitsREL
	case evAbs:
		return d.bitsABS
	case evMsc:
		return d.bitsMSC
	case evSw:
		return d.bitsSW
	case evLed:
		return d.bitsLED
	case evSnd:
		return d.bitsSND
	case evFf:
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
	type inputEvent struct {
		_ [16]byte // TODO: Add timestamp support.
		InputEvent
	}
	var ev [unsafe.Sizeof(inputEvent{})]byte
	_, err := io.ReadFull(d.file, ev[:])
	if err != nil {
		return InputEvent{}, fmt.Errorf("read: %w", err)
	}

	return (*inputEvent)(unsafe.Pointer(&ev[0])).InputEvent, nil
}

type InputEvent struct {
	Type  uint16
	Code  uint16
	Value int32
}

func (ev InputEvent) Is(t, code uint16) bool {
	return (ev.Type == t) && (ev.Code == code)
}

type InputID struct {
	BusType uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type inputAbsInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
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

func fromNTString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return unsafe.String(&b[0], i)
		}
	}

	return unsafe.String(&b[0], len(b))
}
