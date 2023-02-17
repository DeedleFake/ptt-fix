package main

/*
#include <linux/input.h>
*/
import "C"
import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

type Device struct {
	file *os.File

	Name string
}

func OpenDevice(path string) (*Device, error) {
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

	return control(conn, func(fd uintptr) error {
		panic("Not implemented.")
	})
}

func (d *Device) Close() error {
	return d.file.Close()
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

func control(conn syscall.RawConn, f func(uintptr) error) error {
	var ferr error
	err := conn.Control(func(fd uintptr) { ferr = f(fd) })
	return errors.Join(err, ferr)
}
