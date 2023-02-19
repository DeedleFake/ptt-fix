package evdev

import "unsafe"

const (
	wordbits = unsafe.Sizeof(uintptr(0)) * 8

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits

	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14
	iocDirBits  = 2

	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	iocReadEBase = (iocRead << iocDirShift) | ('E' << iocTypeShift)
)

const (
	eviocgversion = iocReadEBase | ((iota + 0x01) << iocNRShift) | (unsafe.Sizeof(int32(0)) << iocSizeShift)
	eviocgid      = iocReadEBase | ((iota + 0x01) << iocNRShift) | (unsafe.Sizeof(InputID{}) << iocSizeShift)
	eviocgrep     = iocReadEBase | ((iota + 0x01) << iocNRShift) | (unsafe.Sizeof([2]uint32{}) << iocSizeShift)
)

const (
	eviocgnameBase = iocReadEBase | ((iota + 0x06) << iocNRShift)
	eviocgphysBase
	eviocguniqBase
	eviocgpropBase
)

const (
	eviocgkeyBase = iocReadEBase | ((0x18 + iota) << iocNRShift)
	eviocgledBase
	eviocgsndBase
	eviocgswBase
)

const (
	eviocgabsBase = iocReadEBase | (unsafe.Sizeof(inputAbsInfo{}) << iocSizeShift)
)

const (
	evCount  = 0x1F + 1
	synCount = 0xF + 1
	keyCount = 0x2FF + 1
	relCount = 0x0F + 1
	absCount = 0x3F + 1
	swCount  = 0x10 + 1
	mscCount = 0x07 + 1
	ledCount = 0x0F + 1
	repCount = 0x01 + 1
	sndCount = 0x07 + 1
	ffCount  = 0x7F + 1
)

const (
	evSyn = iota
	evKey
	evRel
	evAbs
	evMsc
	evSw
)

const (
	evLed = 0x11 + iota
	evSnd
)

const (
	evRep = 0x14 + iota
	evFf
	evPwr
	evFfStatus
)

func eviocgname(length uintptr) uintptr {
	return eviocgnameBase | (length << iocSizeShift)
}

func eviocgbit(ev, length uintptr) uintptr {
	return iocReadEBase | ((0x20 + ev) << iocNRShift) | (length << iocSizeShift)
}
