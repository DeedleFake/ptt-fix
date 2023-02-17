package main

/*
#cgo pkg-config: libevdev libxdo

#include <malloc.h>
#include <errno.h>
#include <libevdev/libevdev.h>
#include <xdo.h>
*/
import "C"
import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sync/errgroup"
)

const (
	eventInvalid = iota
	eventUp
	eventDown
)

func listen(ctx context.Context, device string, keycode C.uint, out chan<- int) error {
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

	err = control(conn, func(fd uintptr) error {
		go func() {
			<-ctx.Done()
			syscall.Close(int(fd))
		}()

		var dev *C.struct_libevdev
		_, err := C.libevdev_new_from_fd(C.int(fd), &dev)
		if err != nil {
			log.Printf("ignoring %q because of error in libevdev init: %v", device, err)
			return nil
		}
		defer C.libevdev_free(dev)

		devname := C.GoString(C.libevdev_get_name(dev))
		log.Printf(
			"initialized input device %q, name: %q, bus: %x, vendor: %x, product: %x",
			device,
			devname,
			C.libevdev_get_id_bustype(dev),
			C.libevdev_get_id_vendor(dev),
			C.libevdev_get_id_product(dev),
		)

		if C.libevdev_has_event_code(dev, C.EV_KEY, keycode) == 0 {
			log.Printf("device %q (%v) is not capable of sending requested key code, ignoring", device, devname)
			return nil
		}

		for {
			var ev C.struct_input_event
			rc := C.libevdev_next_event(dev, C.LIBEVDEV_READ_FLAG_NORMAL, &ev)
			switch rc {
			case C.LIBEVDEV_READ_STATUS_SYNC, C.LIBEVDEV_READ_STATUS_SUCCESS, -C.EAGAIN:
			default:
				return ctx.Err()
			}

			if C.libevdev_event_is_code(&ev, C.EV_KEY, keycode) == 0 {
				continue
			}

			switch ev.value {
			case 2:
			case 1:
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- eventDown:
				}
			default:
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- eventUp:
				}
			}
		}
	})
	if err != nil {
		return fmt.Errorf("listen for events from %q: %w", device, err)
	}

	return nil
}

func handle(ctx context.Context, xdo *C.struct_xdo, key *C.char, ev <-chan int) error {
	defer C.xdo_free(xdo)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-ev:
			switch ev {
			case eventUp:
				C.xdo_send_keysequence_window_up(xdo, C.CURRENTWINDOW, key, 0)
				log.Printf("deactivated")
			case eventDown:
				C.xdo_send_keysequence_window_down(xdo, C.CURRENTWINDOW, key, 0)
				log.Println("activated")
			default:
				return fmt.Errorf("invalid event: %v", ev)
			}
		}
	}
}

func findDevices() ([]string, error) {
	files, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, err
	}

	devices := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), "event") {
			continue
		}

		devices = append(devices, filepath.Join("/dev/input", f.Name()))
	}

	return devices, nil
}

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [/dev/input/by-id/<device>...]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	key := flag.Uint("key", 56, "keycode to watch for")
	sym := flag.String("sym", "Alt_L", "key symbol to send to X")
	flag.Parse()

	devices := flag.Args()
	if len(devices) == 0 {
		d, err := findDevices()
		if err != nil {
			return fmt.Errorf("find devices: %w", err)
		}
		if len(d) == 0 {
			return errors.New("no devices found")
		}

		devices = d
	}

	xdo := C.xdo_new(nil)
	if xdo == nil {
		return errors.New("initialize xdo")
	}

	xdokey := C.CString(*sym)
	defer C.free(unsafe.Pointer(xdokey))

	eg, ctx := errgroup.WithContext(ctx)

	ev := make(chan int)
	for _, dev := range devices {
		dev := dev
		eg.Go(func() error { return listen(ctx, dev, C.uint(*key), ev) })
	}
	eg.Go(func() error { return handle(ctx, xdo, xdokey, ev) })

	err := eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func main() {
	if addr := os.Getenv("PPROF_ADDR"); addr != "" {
		go func() { log.Fatalln(http.ListenAndServe(addr, nil)) }()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := run(ctx)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}
