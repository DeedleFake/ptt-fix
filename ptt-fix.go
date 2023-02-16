package main

/*
#cgo pkg-config: libevdev
#cgo LDFLAGS: -lxdo

#include <errno.h>
#include <libevdev/libevdev.h>
#include <xdo.h>
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
)

const (
	KeyCode   = C.KEY_COMMA
	XKeyEvent = "comma"
)

var cxkeyEvent = C.CString(XKeyEvent)

const (
	eventInvalid = iota
	eventUp
	eventDown
)

func listen(ctx context.Context, device string, out chan<- int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	file, err := os.Open(device)
	if err != nil {
		return err
	}
	defer file.Close()

	conn, err := file.SyscallConn()
	if err != nil {
		return fmt.Errorf("syscall conn for %q: %w", device, err)
	}

	var lerr error
	err = conn.Control(func(fd uintptr) {
		go func() {
			<-ctx.Done()
			syscall.Close(int(fd))
		}()

		var dev *C.struct_libevdev
		_, err := C.libevdev_new_from_fd(C.int(fd), &dev)
		if err != nil {
			lerr = fmt.Errorf("init libevdev for %q: %w", err)
			return
		}
		defer C.libevdev_free(dev)

		devname := C.GoString(C.libevdev_get_name(dev))
		log.Printf(
			"input device name: %q, bus: %x, vendor: %x, product: %x",
			devname,
			C.libevdev_get_id_bustype(dev),
			C.libevdev_get_id_vendor(dev),
			C.libevdev_get_id_product(dev),
		)

		if C.libevdev_has_event_code(dev, C.EV_KEY, KeyCode) == 0 {
			lerr = fmt.Errorf("device %q is not capable of sending key code", devname)
			return
		}

		for {
			var ev C.struct_input_event
			rc := C.libevdev_next_event(dev, C.LIBEVDEV_READ_FLAG_NORMAL, &ev)
			switch rc {
			case C.LIBEVDEV_READ_STATUS_SYNC, C.LIBEVDEV_READ_STATUS_SUCCESS, -C.EAGAIN:
			default:
				lerr = ctx.Err()
				return
			}

			if (ev._type != C.EV_KEY) || (ev.code != KeyCode) {
				continue
			}

			switch ev.value {
			case 2:
			case 1:
				select {
				case <-ctx.Done():
					lerr = ctx.Err()
					return
				case out <- eventDown:
				}
			default:
				select {
				case <-ctx.Done():
					lerr = ctx.Err()
					return
				case out <- eventUp:
				}
			}
		}
	})
	err = errors.Join(err, lerr)
	if err != nil {
		return fmt.Errorf("control %q: %w", device, err)
	}

	return nil
}

func handle(ctx context.Context, ev <-chan int) error {
	xdo := C.xdo_new(nil)
	if xdo == nil {
		return errors.New("initialize xdo")
	}
	defer C.xdo_free(xdo)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-ev:
			switch ev {
			case eventUp:
				C.xdo_send_keysequence_window_up(xdo, C.CURRENTWINDOW, cxkeyEvent, 0)
			case eventDown:
				C.xdo_send_keysequence_window_down(xdo, C.CURRENTWINDOW, cxkeyEvent, 0)
			default:
				return fmt.Errorf("invalid event: %v", ev)
			}
		}
	}
}

func run(ctx context.Context) error {
	devices := os.Args[1:]
	if len(devices) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %v /dev/input/by-id/<device>...\n", os.Args[0])
		os.Exit(2)
	}

	eg, ctx := errgroup.WithContext(ctx)

	ev := make(chan int)
	for _, dev := range devices {
		dev := dev
		eg.Go(func() error { return listen(ctx, dev, ev) })
	}
	eg.Go(func() error { return handle(ctx, ev) })

	err := eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := run(ctx)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}
